package network

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/tolelom/tolchain/core"
)

// MessageHandler is called for each received message.
type MessageHandler func(peer *Peer, msg Message)

// Node listens for incoming peers and manages outgoing connections.
type Node struct {
	nodeID     string
	listenAddr string
	mempool    *core.Mempool

	mu       sync.RWMutex
	peers    map[string]*Peer
	handlers map[MsgType]MessageHandler

	listener net.Listener
	stopCh   chan struct{}
}

// NewNode creates a Node that will listen on listenAddr.
func NewNode(nodeID, listenAddr string, mempool *core.Mempool) *Node {
	n := &Node{
		nodeID:     nodeID,
		listenAddr: listenAddr,
		mempool:    mempool,
		peers:      make(map[string]*Peer),
		handlers:   make(map[MsgType]MessageHandler),
		stopCh:     make(chan struct{}),
	}
	// Register default handlers
	n.Handle(MsgTx, n.handleTx)
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
	ln, err := net.Listen("tcp", n.listenAddr)
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
	peer, err := Connect(id, addr)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.peers[id] = peer
	n.mu.Unlock()
	go n.readLoop(peer)

	// Send hello
	hello, _ := json.Marshal(map[string]string{"node_id": n.nodeID})
	_ = peer.Send(Message{Type: MsgHello, Payload: hello})
	return nil
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
		_ = p.Send(msg)
	}
}

// BroadcastTx serialises tx and sends it to all peers.
func (n *Node) BroadcastTx(tx *core.Transaction) {
	data, err := json.Marshal(tx)
	if err != nil {
		log.Printf("[network] marshal tx: %v", err)
		return
	}
	n.Broadcast(Message{Type: MsgTx, Payload: data})
}

// BroadcastBlock serialises block and sends it to all peers.
func (n *Node) BroadcastBlock(block *core.Block) {
	data, err := json.Marshal(block)
	if err != nil {
		log.Printf("[network] marshal block: %v", err)
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
				log.Printf("[network] accept error: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		peer := NewPeer(conn.RemoteAddr().String(), conn.RemoteAddr().String(), conn)
		n.mu.Lock()
		n.peers[peer.ID] = peer
		n.mu.Unlock()
		go n.readLoop(peer)
	}
}

func (n *Node) readLoop(peer *Peer) {
	defer func() {
		peer.Close()
		n.mu.Lock()
		delete(n.peers, peer.ID)
		n.mu.Unlock()
	}()
	for {
		msg, err := peer.Receive()
		if err != nil {
			return
		}
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
		return
	}
	_ = n.mempool.Add(&tx) // silently drop duplicates / invalid txs
}
