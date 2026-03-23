package core

import (
	"testing"

	"github.com/tolelom/tolchain/crypto"
)

func TestTransaction_SignAndVerify(t *testing.T) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pubHex := pub.Hex()
	tx, err := NewTransaction("test-chain", TxTransfer, pubHex, 0, 10, TransferPayload{To: pubHex, Amount: 100})
	if err != nil {
		t.Fatal(err)
	}
	tx.Sign(priv)

	if tx.ID == "" {
		t.Fatal("ID should be set after Sign")
	}
	if tx.Signature == "" {
		t.Fatal("Signature should be set after Sign")
	}
	if err := tx.Verify(); err != nil {
		t.Fatalf("Verify failed on valid tx: %v", err)
	}
}

func TestTransaction_VerifyFails_TamperedPayload(t *testing.T) {
	priv, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx, _ := NewTransaction("test-chain", TxTransfer, pubHex, 0, 10, TransferPayload{To: pubHex, Amount: 100})
	tx.Sign(priv)

	// Tamper with payload after signing.
	tx.Fee = 999

	if err := tx.Verify(); err == nil {
		t.Fatal("Verify should fail on tampered tx")
	}
}

func TestTransaction_VerifyFails_WrongSigner(t *testing.T) {
	_, pub1, _ := crypto.GenerateKeyPair()
	priv2, _, _ := crypto.GenerateKeyPair()
	pub1Hex := pub1.Hex()

	tx, _ := NewTransaction("test-chain", TxTransfer, pub1Hex, 0, 0, TransferPayload{To: pub1Hex, Amount: 1})
	// Sign with wrong key.
	tx.Sign(priv2)
	// Fix From back to pub1 (ID will mismatch).
	tx.From = pub1Hex

	if err := tx.Verify(); err == nil {
		t.Fatal("Verify should fail when signed by wrong key")
	}
}

func TestTransaction_VerifyFails_EmptyFrom(t *testing.T) {
	tx := &Transaction{From: ""}
	if err := tx.Verify(); err == nil {
		t.Fatal("Verify should fail on empty From")
	}
}

func TestTransaction_Hash_Deterministic(t *testing.T) {
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx, _ := NewTransaction("test", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})

	h1 := tx.Hash()
	h2 := tx.Hash()
	if h1 != h2 {
		t.Fatal("Hash should be deterministic")
	}
}

func TestTransaction_Hash_DifferentForDifferentPayloads(t *testing.T) {
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx1, _ := NewTransaction("test", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})
	tx2, _ := NewTransaction("test", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 2})

	if tx1.Hash() == tx2.Hash() {
		t.Fatal("Different payloads should produce different hashes")
	}
}

func TestTransaction_ChainID_DifferentHash(t *testing.T) {
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx1, _ := NewTransaction("chain-a", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})
	tx2, _ := NewTransaction("chain-b", TxTransfer, pubHex, 0, 0, TransferPayload{To: pubHex, Amount: 1})

	if tx1.Hash() == tx2.Hash() {
		t.Fatal("Different ChainIDs should produce different hashes")
	}
}

func TestHash_OnBehalfOf_BackwardCompatible(t *testing.T) {
	tx1, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2.OnBehalfOf = ""
	if tx1.Hash() != tx2.Hash() {
		t.Error("empty OnBehalfOf changed the hash")
	}
}

func TestHash_OnBehalfOf_ChangesHash(t *testing.T) {
	tx1, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2, _ := NewTransaction("chain", TxTransfer, "aabb", 0, 0, TransferPayload{To: "ccdd", Amount: 100})
	tx2.OnBehalfOf = "eeff1122"
	if tx1.Hash() == tx2.Hash() {
		t.Error("OnBehalfOf should change the hash")
	}
}
