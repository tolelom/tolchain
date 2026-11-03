package network

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"sync"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/metrics"
)

// maxPeerMsgsPerSec disconnects a peer that floods us with messages.
const maxPeerMsgsPerSec = 100

type MessageHandler func(peer *Peer, msg Message)

// Node accepts inbound peers, dials outbound ones and dispatches their
// messages to registered handlers.
type Node struct {
	nodeID          string
	listenAddr      string
	chainID         string
	mempool         *core.Mempool
	tlsConfig       *tls.Config // nil → plain TCP
	maxPeers        int
	writeTimeoutSec int
	readTimeoutSec  int
	maxMessageSize  int

	mu       sync.RWMutex
	peers    map[string]*Peer
	handlers map[MsgType]MessageHandler

	listener net.Listener
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewNode creates a node; a non-nil tlsCfg makes both directions use TLS.
func NewNode(nodeID, listenAddr, chainID string, mempool *core.Mempool, tlsCfg *tls.Config, maxPeers, writeTimeoutSec, readTimeoutSec, maxMessageSize int) *Node {
	n := &Node{
		nodeID:          nodeID,
		listenAddr:      listenAddr,
		chainID:         chainID,
		mempool:         mempool,
		tlsConfig:       tlsCfg,
		maxPeers:        maxPeers,
		writeTimeoutSec: writeTimeoutSec,
		readTimeoutSec:  readTimeoutSec,
		maxMessageSize:  maxMessageSize,
		peers:           make(map[string]*Peer),
		handlers:        make(map[MsgType]MessageHandler),
		stopCh:          make(chan struct{}),
	}
	n.Handle(MsgTx, n.handleTx)
	n.Handle(MsgPing, n.handlePing)
	return n
}

func (n *Node) Handle(typ MsgType, h MessageHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handlers[typ] = h
}

func (n *Node) Start() error {
	var ln net.Listener
	var err error
	if n.tlsConfig != nil {
		ln, err = tls.Listen("tcp", n.listenAddr, n.tlsConfig)
	} else {
		ln, err = net.Listen("tcp", n.listenAddr)
	}
	if err != nil {
		return fmt.Errorf("listen %s: %w", n.listenAddr, err)
	}
	n.listener = ln
	go n.acceptLoop()
	return nil
}

func (n *Node) Stop() {
	n.stopOnce.Do(func() { close(n.stopCh) })
	if n.listener != nil {
		_ = n.listener.Close()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.peers {
		p.Close()
	}
}

// AddPeer dials addr, registers the peer under id and sends hello.
func (n *Node) AddPeer(id, addr string) error {
	peer, err := Connect(id, addr, n.tlsConfig, n.writeTimeoutSec, n.readTimeoutSec, n.maxMessageSize)
	if err != nil {
		return err
	}
	n.mu.Lock()
	if old, exists := n.peers[id]; exists && old != peer {
		old.Close()
	}
	n.peers[id] = peer
	n.mu.Unlock()
	metrics.PeersConnected.Inc()
	go n.readLoop(peer)

	hello, err := json.Marshal(map[string]string{"node_id": n.nodeID})
	if err != nil {
		slog.Error("marshal hello", "component", "network", "error", err)
		return fmt.Errorf("marshal hello: %w", err)
	}
	if err := peer.Send(Message{Type: MsgHello, Payload: hello}); err != nil {
		slog.Error("send hello", "component", "network", "peer", id, "error", err)
	}
	return nil
}

// Peer returns nil when no peer has that id.
func (n *Node) Peer(id string) *Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.peers[id]
}

// Listener exposes the bound address when started on ":0".
func (n *Node) Listener() net.Listener {
	return n.listener
}

// ReKeyPeer moves a peer from its provisional ID to the node_id it announced
// in hello. An older connection already registered under newID is closed and
// replaced.
func (n *Node) ReKeyPeer(oldID, newID string) {
	if oldID == newID {
		return
	}
	n.mu.Lock()
	p, ok := n.peers[oldID]
	if !ok {
		n.mu.Unlock()
		return
	}
	delete(n.peers, oldID)
	if existing, conflict := n.peers[newID]; conflict && existing != p {
		slog.Warn("ReKeyPeer: replacing existing peer with same node_id",
			"component", "network", "old_id", oldID, "new_id", newID, "existing_addr", existing.Addr)
		existing.Close()
	}
	p.setID(newID)
	n.peers[newID] = p
	n.mu.Unlock()
}

func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

func (n *Node) Broadcast(msg Message) {
	n.mu.RLock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.RUnlock()
	for _, p := range peers {
		if err := p.Send(msg); err != nil {
			slog.Error("broadcast failed", "component", "network", "peer", p.ID(), "error", err)
		}
	}
}

func (n *Node) BroadcastTx(tx *core.Transaction) {
	data, err := json.Marshal(tx)
	if err != nil {
		slog.Error("marshal tx", "component", "network", "error", err)
		return
	}
	n.Broadcast(Message{Type: MsgTx, Payload: data})
}

func (n *Node) BroadcastBlock(block *core.Block) {
	data, err := json.Marshal(block)
	if err != nil {
		slog.Error("marshal block", "component", "network", "error", err)
		return
	}
	n.Broadcast(Message{Type: MsgBlock, Payload: data})
}

func (n *Node) acceptLoop() {
	for {
		conn, err := n.listener.Accept()
		if err != nil {
			select {
			case <-n.stopCh:
				return
			default:
				slog.Error("accept error", "component", "network", "error", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		peer, err := n.registerInbound(conn)
		if err != nil {
			slog.Warn("inbound connection rejected", "component", "network", "remote", conn.RemoteAddr(), "reason", err)
			_ = conn.Close()
			continue
		}
		metrics.PeersConnected.Inc()
		go n.readLoop(peer)
	}
}

// registerInbound keys a new connection by its remote address until hello
// tells us the node_id.
func (n *Node) registerInbound(conn net.Conn) (*Peer, error) {
	id := conn.RemoteAddr().String()
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.peers) >= n.maxPeers {
		return nil, fmt.Errorf("max peers (%d) reached", n.maxPeers)
	}
	if _, exists := n.peers[id]; exists {
		return nil, errors.New("duplicate peer connection")
	}
	peer := NewPeer(id, id, conn, n.writeTimeoutSec, n.readTimeoutSec, n.maxMessageSize)
	n.peers[id] = peer
	return peer, nil
}

func (n *Node) readLoop(peer *Peer) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("readLoop panic", "component", "network", "peer", peer.ID(), "panic", r, "stack", string(debug.Stack()))
		}
		peer.Close()
		n.mu.Lock()
		// After a ReKeyPeer collision the key may already hold the live peer.
		if n.peers[peer.ID()] == peer {
			delete(n.peers, peer.ID())
		}
		n.mu.Unlock()
		metrics.PeersConnected.Dec()
	}()

	var msgCount int
	windowStart := time.Now()
	for {
		msg, err := peer.Receive()
		if err != nil {
			return
		}

		msgCount++
		if time.Since(windowStart) >= time.Second {
			msgCount = 1
			windowStart = time.Now()
		} else if msgCount > maxPeerMsgsPerSec {
			slog.Warn("peer rate limit exceeded, disconnecting", "component", "network", "peer", peer.ID(), "msgs_per_sec", msgCount)
			return
		}

		metrics.P2PMessagesReceived.WithLabelValues(string(msg.Type)).Inc()
		n.mu.RLock()
		h, ok := n.handlers[msg.Type]
		n.mu.RUnlock()
		if ok {
			h(peer, msg)
		}
	}
}

func (n *Node) handleTx(_ *Peer, msg Message) {
	var tx core.Transaction
	if err := json.Unmarshal(msg.Payload, &tx); err != nil {
		slog.Error("unmarshal tx", "component", "network", "error", err)
		return
	}
	if n.chainID != "" && tx.ChainID != n.chainID {
		slog.Warn("rejecting tx: chain_id mismatch", "component", "network", "tx_id", tx.ID, "got", tx.ChainID, "want", n.chainID)
		return
	}
	if err := n.mempool.Add(&tx); err != nil {
		slog.Error("mempool add", "component", "network", "error", err)
	}
}

func (n *Node) handlePing(peer *Peer, _ Message) {
	if err := peer.Send(Message{Type: MsgPong}); err != nil {
		slog.Error("pong failed", "component", "network", "peer", peer.ID(), "error", err)
	}
}
