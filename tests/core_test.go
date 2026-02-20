package tests

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/wallet"
)

// TestKeyGenAndAddress verifies that key generation and address derivation work.
func TestKeyGenAndAddress(t *testing.T) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(pub.Hex()) != 64 {
		t.Errorf("pubkey hex length: got %d want 64", len(pub.Hex()))
	}
	addr := pub.Address()
	if len(addr) != 40 {
		t.Errorf("address length: got %d want 40", len(addr))
	}
	// Roundtrip: derived public key should match
	derived := priv.Public()
	if derived.Hex() != pub.Hex() {
		t.Error("derived public key does not match")
	}
}

// TestSignVerify ensures Sign/Verify round-trips correctly.
func TestSignVerify(t *testing.T) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("hello tolchain")
	sig := crypto.Sign(priv, data)
	if err := crypto.Verify(pub, data, sig); err != nil {
		t.Errorf("valid signature failed: %v", err)
	}
	if err := crypto.Verify(pub, []byte("tampered"), sig); err == nil {
		t.Error("tampered data should fail verification")
	}
}

// TestTransactionSignVerify ensures transaction signing and verification work.
func TestTransactionSignVerify(t *testing.T) {
	w, err := wallet.Generate()
	if err != nil {
		t.Fatal(err)
	}

	tx, err := w.NewTx("test-chain", core.TxTransfer, 0, 0, core.TransferPayload{
		To:     "deadbeef",
		Amount: 100,
	})
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	if tx.ID == "" {
		t.Error("tx ID should be set after signing")
	}
	if err := tx.Verify(); err != nil {
		t.Errorf("Verify failed: %v", err)
	}

	// Tamper with the amount to check that verification catches it.
	tx.Fee = 999
	if err := tx.Verify(); err == nil {
		t.Error("tampered tx should fail verification")
	}
}

// TestBlockHash ensures that hashing a block is deterministic.
func TestBlockHash(t *testing.T) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	block := core.NewBlock(1, "0000", pub.Hex(), nil)
	block.Sign(priv)

	if block.Hash == "" {
		t.Error("hash should be set after signing")
	}
	// Re-compute and compare
	if block.ComputeHash() != block.Hash {
		t.Error("ComputeHash() does not match stored hash")
	}
}

// TestMempool verifies add/remove/pending operations.
func TestMempool(t *testing.T) {
	mp := core.NewMempool()
	w, _ := wallet.Generate()

	tx, _ := w.NewTx("test-chain", core.TxTransfer, 0, 0, core.TransferPayload{To: "aa", Amount: 1})
	if err := mp.Add(tx); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if mp.Size() != 1 {
		t.Errorf("size: got %d want 1", mp.Size())
	}
	// Duplicate should fail
	if err := mp.Add(tx); err == nil {
		t.Error("adding duplicate tx should fail")
	}

	pending := mp.Pending(10)
	if len(pending) != 1 {
		t.Errorf("pending: got %d want 1", len(pending))
	}

	mp.Remove([]string{tx.ID})
	if mp.Size() != 0 {
		t.Error("pool should be empty after remove")
	}
}
