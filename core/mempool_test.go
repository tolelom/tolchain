package core

import (
	"testing"
	"time"

	"github.com/tolelom/tolchain/crypto"
)

// helper: create a valid signed transaction.
func newSignedTx(t *testing.T, chainID string, nonce uint64) *Transaction {
	t.Helper()
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pubHex := pub.Hex()
	tx, err := NewTransaction(chainID, TxTransfer, pubHex, nonce, 0, TransferPayload{To: pubHex, Amount: 1})
	if err != nil {
		t.Fatal(err)
	}
	tx.Sign(priv)
	return tx
}

func TestMempool_AddAndGet(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	tx := newSignedTx(t, "test", 0)

	if err := mp.Add(tx); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if mp.Size() != 1 {
		t.Fatalf("expected size 1, got %d", mp.Size())
	}
	got, ok := mp.Get(tx.ID)
	if !ok || got.ID != tx.ID {
		t.Fatal("Get returned wrong tx or not found")
	}
}

func TestMempool_DuplicateRejected(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	tx := newSignedTx(t, "test", 0)
	mp.Add(tx)

	if err := mp.Add(tx); err == nil {
		t.Fatal("expected error for duplicate tx")
	}
}

func TestMempool_DuplicateNonceRejected(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	priv, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	tx1, _ := NewTransaction("test", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})
	tx1.Sign(priv)
	mp.Add(tx1)

	tx2, _ := NewTransaction("test", TxTransfer, pubHex, 0, 1, TransferPayload{To: pubHex, Amount: 2})
	tx2.Sign(priv)

	if err := mp.Add(tx2); err == nil {
		t.Fatal("expected error for duplicate nonce")
	}
}

func TestMempool_ExpiredRejected(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	priv, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx, _ := NewTransaction("test", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})
	tx.Timestamp = time.Now().Add(-2 * time.Hour).UnixNano()
	tx.Sign(priv)

	if err := mp.Add(tx); err == nil {
		t.Fatal("expected error for expired tx")
	}
}

func TestMempool_FutureTsRejected(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	priv, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx, _ := NewTransaction("test", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})
	tx.Timestamp = time.Now().Add(10 * time.Minute).UnixNano()
	tx.Sign(priv)

	if err := mp.Add(tx); err == nil {
		t.Fatal("expected error for future timestamp")
	}
}

func TestMempool_InvalidSigRejected(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	tx := newSignedTx(t, "test", 0)
	tx.Signature = "0000" // corrupt signature

	if err := mp.Add(tx); err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestMempool_Pending_Order(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	tx1 := newSignedTx(t, "test", 0)
	tx2 := newSignedTx(t, "test", 0)
	mp.Add(tx1)
	mp.Add(tx2)

	pending := mp.Pending(10)
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
	if pending[0].ID != tx1.ID || pending[1].ID != tx2.ID {
		t.Fatal("pending order does not match insertion order")
	}
}

func TestMempool_Pending_Limit(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	for i := 0; i < 5; i++ {
		mp.Add(newSignedTx(t, "test", uint64(i)))
	}
	pending := mp.Pending(3)
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(pending))
	}
}

func TestMempool_Remove(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	tx1 := newSignedTx(t, "test", 0)
	tx2 := newSignedTx(t, "test", 0)
	mp.Add(tx1)
	mp.Add(tx2)

	mp.Remove([]string{tx1.ID})

	if mp.Size() != 1 {
		t.Fatalf("expected size 1 after remove, got %d", mp.Size())
	}
	if _, ok := mp.Get(tx1.ID); ok {
		t.Fatal("removed tx should not be found")
	}
	if _, ok := mp.Get(tx2.ID); !ok {
		t.Fatal("non-removed tx should still be found")
	}
}

func TestMempool_Remove_FreesNonce(t *testing.T) {
	mp := NewMempool(10000, 3600, 300)
	priv, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	tx1, _ := NewTransaction("test", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})
	tx1.Sign(priv)
	mp.Add(tx1)

	mp.Remove([]string{tx1.ID})

	// Same nonce should now be accepted.
	tx2, _ := NewTransaction("test", TxTransfer, pubHex, 0, 1, TransferPayload{To: pubHex, Amount: 2})
	tx2.Sign(priv)

	if err := mp.Add(tx2); err != nil {
		t.Fatalf("expected nonce to be freed after remove: %v", err)
	}
}

func newDelegatedTx(t *testing.T, signer crypto.PrivateKey, onBehalfOf string, nonce uint64) *Transaction {
	t.Helper()
	from := signer.Public().Hex()
	tx, err := NewTransaction("test", TxTransfer, from, nonce, 0, TransferPayload{To: from, Amount: 1})
	if err != nil {
		t.Fatal(err)
	}
	tx.OnBehalfOf = onBehalfOf
	tx.Sign(signer)
	return tx
}

// A custodial grantee signs for many granters; each granter has its own nonce
// sequence, so the same nonce for different granters is not a duplicate.
func TestMempool_DelegatedNonceIsPerGranter(t *testing.T) {
	mp := NewMempool(100, 3600, 300)
	grantee, _, _ := crypto.GenerateKeyPair()
	_, granterA, _ := crypto.GenerateKeyPair()
	_, granterB, _ := crypto.GenerateKeyPair()

	if err := mp.Add(newDelegatedTx(t, grantee, granterA.Hex(), 0)); err != nil {
		t.Fatalf("first delegated tx: %v", err)
	}
	if err := mp.Add(newDelegatedTx(t, grantee, granterB.Hex(), 0)); err != nil {
		t.Fatalf("same nonce for a different granter must be accepted: %v", err)
	}
}

// A delegated tx spends the granter's nonce, so it collides with the granter's
// own tx at the same nonce.
func TestMempool_DelegatedNonceCollidesWithGranter(t *testing.T) {
	mp := NewMempool(100, 3600, 300)
	grantee, _, _ := crypto.GenerateKeyPair()
	granter, granterPub, _ := crypto.GenerateKeyPair()

	own, err := NewTransaction("test", TxTransfer, granterPub.Hex(), 0, 0, TransferPayload{To: granterPub.Hex(), Amount: 1})
	if err != nil {
		t.Fatal(err)
	}
	own.Sign(granter)
	if err := mp.Add(own); err != nil {
		t.Fatal(err)
	}
	delegated := newDelegatedTx(t, grantee, granterPub.Hex(), 0)
	if err := mp.Add(delegated); err == nil {
		t.Fatal("delegated tx reusing the granter's pending nonce must be rejected")
	}
	mp.Remove([]string{own.ID})
	if err := mp.Add(delegated); err != nil {
		t.Fatalf("nonce must be free after the granter's tx is removed: %v", err)
	}
}
