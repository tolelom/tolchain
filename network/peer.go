// Package network handles peer-to-peer communication over TCP using
// length-prefixed JSON messages.
package network

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
)

// MsgType labels a network message.
type MsgType string

const (
	MsgHello     MsgType = "hello"
	MsgTx        MsgType = "tx"
	MsgBlock     MsgType = "block"
	MsgGetBlocks MsgType = "get_blocks"
	MsgBlocks    MsgType = "blocks"
)

// Message is the envelope for all P2P communication.
type Message struct {
	Type    MsgType         `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Peer represents a connected remote node.
type Peer struct {
	ID   string
	Addr string

	conn   net.Conn
	mu     sync.Mutex
	closed bool
}

// NewPeer wraps an established TCP connection as a Peer.
func NewPeer(id, addr string, conn net.Conn) *Peer {
	return &Peer{ID: id, Addr: addr, conn: conn}
}

// Connect dials the remote address and returns a connected Peer.
func Connect(id, addr string) (*Peer, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	return NewPeer(id, addr, conn), nil
}

// Send writes a length-prefixed JSON message to the peer.
func (p *Peer) Send(msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("peer %s closed", p.ID)
	}
	// 4-byte big-endian length prefix
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := p.conn.Write(header[:]); err != nil {
		return err
	}
	_, err = p.conn.Write(data)
	return err
}

// Receive reads the next length-prefixed JSON message.
func (p *Peer) Receive() (Message, error) {
	var header [4]byte
	if _, err := io.ReadFull(p.conn, header[:]); err != nil {
		return Message{}, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > 32*1024*1024 { // 32 MB safety limit
		return Message{}, fmt.Errorf("message too large: %d bytes", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(p.conn, buf); err != nil {
		return Message{}, err
	}
	var msg Message
	if err := json.Unmarshal(buf, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}

// Close terminates the peer connection.
func (p *Peer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		p.conn.Close()
	}
}
