// Package network is the P2P layer: TCP (optionally mTLS) connections
// carrying JSON messages framed by a 4-byte big-endian length.
package network

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type MsgType string

const (
	MsgHello     MsgType = "hello"
	MsgTx        MsgType = "tx"
	MsgBlock     MsgType = "block"
	MsgGetBlocks MsgType = "get_blocks"
	MsgBlocks    MsgType = "blocks"
	MsgPing      MsgType = "ping"
	MsgPong      MsgType = "pong"
)

type Message struct {
	Type    MsgType         `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Peer is a connected remote node.
type Peer struct {
	Addr string

	// id changes once the peer announces its node_id, while other goroutines
	// may be logging it.
	id atomic.Pointer[string]

	conn           net.Conn
	mu             sync.Mutex
	closed         bool
	writeTimeout   time.Duration
	readTimeout    time.Duration
	maxMessageSize uint32
}

func NewPeer(id, addr string, conn net.Conn, writeTimeoutSec, readTimeoutSec, maxMsgSize int) *Peer {
	p := &Peer{
		Addr:           addr,
		conn:           conn,
		writeTimeout:   time.Duration(writeTimeoutSec) * time.Second,
		readTimeout:    time.Duration(readTimeoutSec) * time.Second,
		maxMessageSize: uint32(maxMsgSize),
	}
	p.setID(id)
	return p
}

func (p *Peer) ID() string { return *p.id.Load() }

func (p *Peer) setID(id string) { p.id.Store(&id) }

// Connect dials addr, over TLS when tlsCfg is non-nil.
func Connect(id, addr string, tlsCfg *tls.Config, writeTimeoutSec, readTimeoutSec, maxMsgSize int) (*Peer, error) {
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
	return NewPeer(id, addr, conn, writeTimeoutSec, readTimeoutSec, maxMsgSize), nil
}

func (p *Peer) Send(msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("peer %s closed", p.ID())
	}
	if err := p.conn.SetWriteDeadline(time.Now().Add(p.writeTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if _, err := p.conn.Write(header[:]); err != nil {
		return err
	}
	_, err = p.conn.Write(data)
	return err
}

// Receive reads the next message. The read deadline drops idle peers and the
// size cap is checked before allocating the body.
func (p *Peer) Receive() (Message, error) {
	if err := p.conn.SetReadDeadline(time.Now().Add(p.readTimeout)); err != nil {
		return Message{}, fmt.Errorf("set read deadline: %w", err)
	}
	var header [4]byte
	if _, err := io.ReadFull(p.conn, header[:]); err != nil {
		return Message{}, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > p.maxMessageSize {
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

// Close is idempotent.
func (p *Peer) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		_ = p.conn.Close()
	}
}
