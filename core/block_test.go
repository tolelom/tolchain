package core_test

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// helper: generate a key pair and return (priv, pub, pubHex).
func generateTestKeys(t *testing.T) (crypto.PrivateKey, crypto.PublicKey, string) {
	t.Helper()
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return priv, pub, pub.Hex()
}

// helper: create signed transactions for TxRoot tests.
func makeSignedTxs(t *testing.T, n int) []*core.Transaction {
	t.Helper()
	priv, pub, _ := crypto.GenerateKeyPair()
	if priv == nil {
		t.Fatal("GenerateKeyPair failed")
	}
	pubHex := pub.Hex()
	txs := make([]*core.Transaction, n)
	for i := 0; i < n; i++ {
		tx, err := core.NewTransaction("test", core.TxTransfer, pubHex, uint64(i), 0, core.TransferPayload{To: pubHex, Amount: 1})
		if err != nil {
			t.Fatalf("NewTransaction: %v", err)
		}
		tx.Sign(priv)
		txs[i] = tx
	}
	return txs
}

func TestBlock_NewBlockSetsCorrectHeaderFields(t *testing.T) {
	_, _, pubHex := generateTestKeys(t)

	block := core.NewBlock("chain-1", 5, "prev123", pubHex, nil)

	if block.Header.ChainID != "chain-1" {
		t.Errorf("ChainID = %q, want %q", block.Header.ChainID, "chain-1")
	}
	if block.Header.Height != 5 {
		t.Errorf("Height = %d, want 5", block.Header.Height)
	}
	if block.Header.PrevHash != "prev123" {
		t.Errorf("PrevHash = %q, want %q", block.Header.PrevHash, "prev123")
	}
	if block.Header.Proposer != pubHex {
		t.Errorf("Proposer = %q, want %q", block.Header.Proposer, pubHex)
	}
	if block.Header.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}
	if block.Header.TxRoot == "" {
		t.Error("TxRoot should be set")
	}
	// Hash and Signature are empty before signing.
	if block.Hash != "" {
		t.Errorf("Hash should be empty before signing, got %q", block.Hash)
	}
	if block.Signature != "" {
		t.Errorf("Signature should be empty before signing, got %q", block.Signature)
	}
}

func TestBlock_ComputeHashIsDeterministic(t *testing.T) {
	_, _, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)

	h1 := block.ComputeHash()
	h2 := block.ComputeHash()

	if h1 != h2 {
		t.Errorf("ComputeHash not deterministic: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("ComputeHash returned empty string")
	}
}

func TestBlock_ComputeHashChangesWhenHeaderChanges(t *testing.T) {
	_, _, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)

	h1 := block.ComputeHash()
	block.Header.Height = 1
	h2 := block.ComputeHash()

	if h1 == h2 {
		t.Error("ComputeHash should change when header changes")
	}
}

func TestBlock_SignSetsHashAndSignature(t *testing.T) {
	priv, _, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)

	block.Sign(priv)

	if block.Hash == "" {
		t.Error("Sign should set Hash")
	}
	if block.Signature == "" {
		t.Error("Sign should set Signature")
	}
	// Hash should match ComputeHash.
	if block.Hash != block.ComputeHash() {
		t.Error("Hash after Sign does not match ComputeHash")
	}
}

func TestBlock_VerifySucceedsOnValidBlock(t *testing.T) {
	priv, pub, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)
	block.Sign(priv)

	if err := block.Verify(pub); err != nil {
		t.Errorf("Verify should succeed on valid block: %v", err)
	}
}

func TestBlock_VerifyFailsWhenHeaderTampered(t *testing.T) {
	priv, pub, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)
	block.Sign(priv)

	// Tamper with the header after signing.
	block.Header.Height = 999

	if err := block.Verify(pub); err == nil {
		t.Error("Verify should fail when header is tampered")
	}
}

func TestBlock_VerifyFailsWithWrongPublicKey(t *testing.T) {
	priv, _, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)
	block.Sign(priv)

	// Use a different key pair.
	_, wrongPub, _ := generateTestKeys(t)

	if err := block.Verify(wrongPub); err == nil {
		t.Error("Verify should fail with wrong public key")
	}
}

func TestBlock_VerifyIntegritySucceedsOnValidBlock(t *testing.T) {
	priv, _, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)
	block.Sign(priv)

	if err := block.VerifyIntegrity(); err != nil {
		t.Errorf("VerifyIntegrity should succeed on valid block: %v", err)
	}
}

func TestBlock_VerifyIntegrityFailsWhenHashTampered(t *testing.T) {
	priv, _, pubHex := generateTestKeys(t)
	block := core.NewBlock("test", 0, "", pubHex, nil)
	block.Sign(priv)

	block.Hash = "tampered_hash"

	if err := block.VerifyIntegrity(); err == nil {
		t.Error("VerifyIntegrity should fail when hash is tampered")
	}
}

func TestBlock_VerifyIntegrityFailsWhenTxsModified(t *testing.T) {
	priv, pub, _ := crypto.GenerateKeyPair()
	if priv == nil {
		t.Fatal("GenerateKeyPair failed")
	}
	pubHex := pub.Hex()

	txs := makeSignedTxs(t, 2)
	block := core.NewBlock("test", 0, "", pubHex, txs)
	block.Sign(priv)

	// Sanity: integrity holds before modification.
	if err := block.VerifyIntegrity(); err != nil {
		t.Fatalf("VerifyIntegrity should succeed before modification: %v", err)
	}

	// Modify transactions (add an extra one) so TxRoot no longer matches.
	extraTxs := makeSignedTxs(t, 1)
	block.Transactions = append(block.Transactions, extraTxs...)

	if err := block.VerifyIntegrity(); err == nil {
		t.Error("VerifyIntegrity should fail when transactions are modified")
	}
}

func TestBlock_ComputeTxRootEmptyTxs(t *testing.T) {
	root := core.ComputeTxRoot(nil)
	expected := crypto.Hash([]byte("empty"))
	if root != expected {
		t.Errorf("ComputeTxRoot(nil) = %q, want %q", root, expected)
	}

	root2 := core.ComputeTxRoot([]*core.Transaction{})
	if root2 != expected {
		t.Errorf("ComputeTxRoot([]) = %q, want %q", root2, expected)
	}
}

func TestBlock_ComputeTxRootIsDeterministic(t *testing.T) {
	txs := makeSignedTxs(t, 3)

	r1 := core.ComputeTxRoot(txs)
	r2 := core.ComputeTxRoot(txs)

	if r1 != r2 {
		t.Errorf("ComputeTxRoot not deterministic: %q != %q", r1, r2)
	}
	if r1 == "" {
		t.Error("ComputeTxRoot returned empty string")
	}
}

func TestBlock_ComputeTxRootDiffersForDifferentTxSets(t *testing.T) {
	txs1 := makeSignedTxs(t, 2)
	txs2 := makeSignedTxs(t, 3)

	r1 := core.ComputeTxRoot(txs1)
	r2 := core.ComputeTxRoot(txs2)

	if r1 == r2 {
		t.Error("ComputeTxRoot should differ for different transaction sets")
	}
}
