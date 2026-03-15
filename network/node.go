package network

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/metrics"
)

// MessageHandler is called for each received message.
type MessageHandler func(peer *Peer, msg Message)

// Node listens for incoming peers and manages outgoing connections.
type Node struct {
	nodeID         string
	listenAddr     string
	chainID        string
	mempool        *core.Mempool
	tlsConfig      *tls.Config // nil → plain TCP
	maxPeers       int
	writeTimeoutSec int
	readTimeoutSec  int
	maxMessageSize  int

	mu       sync.RWMutex
	peers    map[string]*Peer
	handlers map[MsgType]MessageHandler

	listener net.Listener
	stopCh   chan struct{}
}

// NewNode creates a Node that will listen on listenAddr.
// If tlsCfg is non-nil the listener and outgoing connections use TLS.
// maxPeers, writeTimeoutSec, readTimeoutSec, maxMessageSize configure network limits.
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
	// Register default handlers
	n.Handle(MsgTx, n.handleTx)
	n.Handle(MsgPing, n.handlePing)
	return n
}

// Handle registers a handler for msg type.
func (n *Node) Handle(typ MsgType, h MessageHandler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handlers[typ] = h
}

// Start begins accepting connections.
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

// Stop shuts down the node.
func (n *Node) Stop() {
	close(n.stopCh)
	if n.listener != nil {
		n.listener.Close()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.peers {
		p.Close()
	}
}

// AddPeer dials addr and registers the peer.
func (n *Node) AddPeer(id, addr string) error {
	peer, err := Connect(id, addr, n.tlsConfig, n.writeTimeoutSec, n.readTimeoutSec, n.maxMessageSize)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.peers[id] = peer
	n.mu.Unlock()
	metrics.PeersConnected.Inc()
	go n.readLoop(peer)

	// Send hello
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

// Peer returns the connected peer with the given id, or nil if not found.
func (n *Node) Peer(id string) *Peer {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.peers[id]
}

// Listener returns the underlying net.Listener. Useful for getting the bound address when started on ":0".
func (n *Node) Listener() net.Listener {
	return n.listener
}

// ReKeyPeer updates a peer's key in the peer map from oldID to newID.
// It also updates the peer's ID field. This is used when a peer announces
// its real node_id via MsgHello.
func (n *Node) ReKeyPeer(oldID, newID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p, ok := n.peers[oldID]
	if !ok {
		return
	}
	delete(n.peers, oldID)
	p.ID = newID
	n.peers[newID] = p
}

// PeerCount returns the number of currently connected peers.
func (n *Node) PeerCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.peers)
}

// Broadcast sends msg to all connected peers.
func (n *Node) Broadcast(msg Message) {
	n.mu.RLock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, p)
	}
	n.mu.RUnlock()
	for _, p := range peers {
		if err := p.Send(msg); err != nil {
			slog.Error("broadcast failed", "component", "network", "peer", p.ID, "error", err)
		}
	}
}

// BroadcastTx serialises tx and sends it to all peers.
func (n *Node) BroadcastTx(tx *core.Transaction) {
	data, err := json.Marshal(tx)
	if err != nil {
		slog.Error("marshal tx", "component", "network", "error", err)
		return
	}
	n.Broadcast(Message{Type: MsgTx, Payload: data})
}

// BroadcastBlock serialises block and sends it to all peers.
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
		n.mu.RLock()
		peerCount := len(n.peers)
		n.mu.RUnlock()
		if peerCount >= n.maxPeers {
			slog.Warn("max peers reached, rejecting connection", "component", "network", "max_peers", n.maxPeers, "remote", conn.RemoteAddr())
			conn.Close()
			continue
		}
		id := conn.RemoteAddr().String()
		n.mu.Lock()
		if _, exists := n.peers[id]; exists {
			n.mu.Unlock()
			conn.Close()
			slog.Warn("duplicate peer connection rejected", "component", "network", "id", id)
			continue
		}
		peer := NewPeer(id, id, conn, n.writeTimeoutSec, n.readTimeoutSec, n.maxMessageSize)
		n.peers[peer.ID] = peer
		n.mu.Unlock()
		metrics.PeersConnected.Inc()
		go n.readLoop(peer)
	}
}

func (n *Node) readLoop(peer *Peer) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("readLoop panic", "component", "network", "peer", peer.ID, "panic", r)
		}
		peer.Close()
		n.mu.Lock()
		delete(n.peers, peer.ID)
		n.mu.Unlock()
		metrics.PeersConnected.Dec()
	}()

	var msgCount int
	lastReset := time.Now()

	for {
		msg, err := peer.Receive()
		if err != nil {
			return
		}

		// Per-peer rate limiting: max 100 messages per second.
		msgCount++
		if time.Since(lastReset) >= time.Second {
			msgCount = 0
			lastReset = time.Now()
		} else if msgCount > 100 {
			slog.Warn("peer rate limit exceeded, disconnecting", "component", "network", "peer", peer.ID, "msgs_per_sec", msgCount)
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
		slog.Error("pong failed", "component", "network", "peer", peer.ID, "error", err)
	}
}
