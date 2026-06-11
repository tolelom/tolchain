package network

import (
	"net"
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
)

func newTestNode() *Node {
	mp := core.NewMempool(100, 3600, 300)
	return NewNode("test-node", ":0", "test-chain", mp, nil, 50, 5, 5, 1<<20)
}

// TestReKeyPeer_CollisionCleanupKeepsLivePeer is a regression test for the
// ghost-peer bug: when ReKeyPeer closes an older colliding connection, the
// dying peer's readLoop cleanup must not delete the LIVE re-keyed peer that
// now occupies the same map key.
func TestReKeyPeer_CollisionCleanupKeepsLivePeer(t *testing.T) {
	n := newTestNode()

	// Peer A: an earlier connection already registered under "node-1".
	connA, remoteA := net.Pipe()
	defer remoteA.Close()
	peerA := NewPeer("node-1", "addrA", connA, 5, 5, 1<<20)

	// Peer B: a newer connection keyed by its remote address, about to
	// announce node_id "node-1" via hello.
	connB, remoteB := net.Pipe()
	defer remoteB.Close()
	peerB := NewPeer("addrB", "addrB", connB, 5, 5, 1<<20)

	n.mu.Lock()
	n.peers[peerA.ID] = peerA
	n.peers[peerB.ID] = peerB
	n.mu.Unlock()

	// Service peer A's connection like acceptLoop would.
	done := make(chan struct{})
	go func() {
		n.readLoop(peerA)
		close(done)
	}()

	// Hello on peer B re-keys addrB -> node-1, closing the colliding peer A.
	n.ReKeyPeer("addrB", "node-1")

	// Peer A's readLoop exits (its conn was closed) and runs deferred cleanup.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit after peer was closed")
	}

	if got := n.Peer("node-1"); got != peerB {
		t.Fatalf("live re-keyed peer was removed from the peer map (ghost peer): got %v", got)
	}
}

// TestReadLoop_CleanupRemovesOwnEntry verifies normal cleanup still works:
// when a peer's connection dies and it is still the registered entry, the
// readLoop removes it from the peer map.
func TestReadLoop_CleanupRemovesOwnEntry(t *testing.T) {
	n := newTestNode()

	conn, remote := net.Pipe()
	peer := NewPeer("node-x", "addrX", conn, 5, 5, 1<<20)

	n.mu.Lock()
	n.peers[peer.ID] = peer
	n.mu.Unlock()

	done := make(chan struct{})
	go func() {
		n.readLoop(peer)
		close(done)
	}()

	remote.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit after remote closed")
	}

	if got := n.Peer("node-x"); got != nil {
		t.Fatalf("dead peer should be removed from the peer map, got %v", got)
	}
}
