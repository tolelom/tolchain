// Package network handles peer-to-peer communication over TCP using
// length-prefixed JSON messages.
package network

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
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
// If tlsCfg is non-nil the connection is established over TLS.
func Connect(id, addr string, tlsCfg *tls.Config) (*Peer, error) {
	var conn net.Conn
	var err error
	if tlsCfg != nil {
		conn, err = tls.Dial("tcp", addr, tlsCfg)
	} else {
		conn, err = net.Dial("tcp", addr)
	}
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
// A 30-second read deadline prevents a stalled peer from blocking indefinitely.
func (p *Peer) Receive() (Message, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
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
