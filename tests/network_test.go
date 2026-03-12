package tests

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/network"
)

func TestPeerSendReceive(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sender := network.NewPeer("sender", "pipe", c1, 30, 300, 10*1024*1024)
	receiver := network.NewPeer("receiver", "pipe", c2, 30, 300, 10*1024*1024)

	payload, _ := json.Marshal(map[string]string{"hello": "world"})
	msg := network.Message{Type: network.MsgPing, Payload: payload}

	done := make(chan network.Message, 1)
	go func() {
		m, err := receiver.Receive()
		if err != nil {
			t.Error(err)
			return
		}
		done <- m
	}()

	if err := sender.Send(msg); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if got.Type != network.MsgPing {
			t.Errorf("type: got %s want ping", got.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestPeerMessageSizeLimit(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	receiver := network.NewPeer("receiver", "pipe", c2, 30, 300, 10*1024*1024)

	// Write a header claiming 11 MB (exceeds 10 MB limit)
	go func() {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], 11*1024*1024)
		c1.Write(header[:])
	}()

	_, err := receiver.Receive()
	if err == nil {
		t.Error("should reject message larger than 10 MB")
	}
}

func TestNodePeerCount(t *testing.T) {
	mempool := core.NewMempool(10000, 3600, 300)
	node := network.NewNode("test", ":0", testChainID, mempool, nil, 50, 30, 300, 10*1024*1024)
	if err := node.Start(); err != nil {
		t.Fatal(err)
	}
	defer node.Stop()

	if node.PeerCount() != 0 {
		t.Errorf("initial peer count: got %d want 0", node.PeerCount())
	}

	// Connect a second node to the first
	node2 := network.NewNode("test2", ":0", testChainID, core.NewMempool(10000, 3600, 300), nil, 50, 30, 300, 10*1024*1024)
	if err := node2.Start(); err != nil {
		t.Fatal(err)
	}
	defer node2.Stop()

	// node2 connects to node
	addr := node.Listener().Addr().String()
	if err := node2.AddPeer("test", addr); err != nil {
		t.Fatal(err)
	}

	// Give time for the accept loop to register the peer
	time.Sleep(100 * time.Millisecond)

	if node.PeerCount() != 1 {
		t.Errorf("peer count after connect: got %d want 1", node.PeerCount())
	}
}
