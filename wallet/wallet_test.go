package wallet

import (
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/tolelom/tolchain/crypto"
)

// ---- Wallet creation ----

func TestGenerate(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if w == nil {
		t.Fatal("Generate returned nil wallet")
	}
	if len(w.PrivKey()) != 64 {
		t.Errorf("PrivKey length = %d, want 64", len(w.PrivKey()))
	}
}

func TestNew_PreservesKeys(t *testing.T) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	w := New(priv)
	if w.PrivKey().Hex() != priv.Hex() {
		t.Error("New did not preserve private key")
	}
	if w.PubKey() != pub.Hex() {
		t.Errorf("PubKey = %s, want %s", w.PubKey(), pub.Hex())
	}
}

func TestPubKey_Length(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	pk := w.PubKey()
	if len(pk) != 64 {
		t.Errorf("PubKey length = %d, want 64", len(pk))
	}
	if _, err := hex.DecodeString(pk); err != nil {
		t.Errorf("PubKey is not valid hex: %v", err)
	}
}

func TestAddress_Length(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	addr := w.Address()
	if len(addr) != 40 {
		t.Errorf("Address length = %d, want 40", len(addr))
	}
	if _, err := hex.DecodeString(addr); err != nil {
		t.Errorf("Address is not valid hex: %v", err)
	}
}

// ---- Transaction building ----

func TestTransfer(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	_, recipientPub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	tx, err := w.Transfer("test-chain", recipientPub.Hex(), 1000, 1, 10)
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if tx.ChainID != "test-chain" {
		t.Errorf("ChainID = %s, want test-chain", tx.ChainID)
	}
	if tx.From != w.PubKey() {
		t.Errorf("From = %s, want %s", tx.From, w.PubKey())
	}
	if tx.Nonce != 1 {
		t.Errorf("Nonce = %d, want 1", tx.Nonce)
	}
	if tx.Fee != 10 {
		t.Errorf("Fee = %d, want 10", tx.Fee)
	}
	if tx.Signature == "" {
		t.Error("Transfer tx has empty signature")
	}
	if tx.ID == "" {
		t.Error("Transfer tx has empty ID")
	}
	// Verify the signature
	if err := tx.Verify(); err != nil {
		t.Errorf("Transfer tx Verify failed: %v", err)
	}
}

func TestNewTx_Transfer(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tx, err := w.NewTx("chain-1", "transfer", 0, 5, map[string]any{
		"to":     "deadbeef",
		"amount": 42,
	})
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	if tx.Type != "transfer" {
		t.Errorf("Type = %s, want transfer", tx.Type)
	}
	if tx.Signature == "" {
		t.Error("NewTx produced unsigned transaction")
	}
}

func TestNewTx_MintAsset(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	tx, err := w.NewTx("chain-1", "mint_asset", 0, 5, map[string]any{
		"template_id": "sword-01",
		"owner":       w.PubKey(),
		"properties":  map[string]any{"damage": 10},
	})
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	if tx.Type != "mint_asset" {
		t.Errorf("Type = %s, want mint_asset", tx.Type)
	}
	if err := tx.Verify(); err != nil {
		t.Errorf("mint_asset tx Verify failed: %v", err)
	}
}

// ---- Keystore ----

func TestSaveKey_LoadKey_Roundtrip(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	password := "s3cret-pass"

	if err := SaveKey(path, password, priv); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}
	loaded, err := LoadKey(path, password)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if loaded.Hex() != priv.Hex() {
		t.Errorf("loaded key = %s, want %s", loaded.Hex(), priv.Hex())
	}
}

func TestLoadKey_WrongPassword(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	if err := SaveKey(path, "correct", priv); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}
	_, err = LoadKey(path, "wrong")
	if err == nil {
		t.Error("expected error for wrong password, got nil")
	}
}

func TestLoadKey_NonexistentFile(t *testing.T) {
	_, err := LoadKey("/nonexistent/path/key.json", "pass")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

// ---- Convenience method tests ----

func TestMintAsset(t *testing.T) {
	w, _ := Generate()
	_, pub2, _ := crypto.GenerateKeyPair()
	tx, err := w.MintAsset("chain", "tmpl1", pub2.Hex(), map[string]any{"damage": 10}, 0, 5)
	if err != nil {
		t.Fatalf("MintAsset: %v", err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tx.Type != "mint_asset" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestBurnAsset(t *testing.T) {
	w, _ := Generate()
	tx, err := w.BurnAsset("chain", "asset1", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "burn_asset" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestTransferAsset(t *testing.T) {
	w, _ := Generate()
	_, pub2, _ := crypto.GenerateKeyPair()
	tx, err := w.TransferAsset("chain", "asset1", pub2.Hex(), 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "transfer_asset" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestRegisterTemplate(t *testing.T) {
	w, _ := Generate()
	tx, err := w.RegisterTemplate("chain", "t1", "Sword", map[string]any{"name": "string"}, true, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "register_template" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestListMarket(t *testing.T) {
	w, _ := Generate()
	tx, err := w.ListMarket("chain", "asset1", 100, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "list_market" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestBuyMarket(t *testing.T) {
	w, _ := Generate()
	tx, err := w.BuyMarket("chain", "listing1", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "buy_market" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestCancelListing(t *testing.T) {
	w, _ := Generate()
	tx, err := w.CancelListing("chain", "listing1", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "cancel_listing" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestEquipItem(t *testing.T) {
	w, _ := Generate()
	tx, err := w.EquipItem("chain", "asset1", "weapon", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "equip_item" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestUnequipItem(t *testing.T) {
	w, _ := Generate()
	tx, err := w.UnequipItem("chain", "asset1", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "unequip_item" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestGrantReward(t *testing.T) {
	w, _ := Generate()
	_, pub2, _ := crypto.GenerateKeyPair()
	tx, err := w.GrantReward("chain", pub2.Hex(), 500, nil, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "grant_reward" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestRandomCommit(t *testing.T) {
	w, _ := Generate()
	tx, err := w.RandomCommit("chain", "rc1", "abc123", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "random_commit" {
		t.Errorf("got type %s", tx.Type)
	}
}

func TestRandomReveal(t *testing.T) {
	w, _ := Generate()
	tx, err := w.RandomReveal("chain", "rc1", "my-secret", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Verify(); err != nil {
		t.Fatal(err)
	}
	if tx.Type != "random_reveal" {
		t.Errorf("got type %s", tx.Type)
	}
}

// ---- Additional coverage tests ----

func TestLoadKey_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(path, []byte("this is not json{{{"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadKey(path, "password")
	if err == nil {
		t.Error("expected error for corrupt JSON file, got nil")
	}
}

func TestLoadKey_InvalidHexSalt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_salt.json")
	data := `{"pub_key":"aabb","salt":"ZZZZ","nonce":"aabb","cipher_text":"aabb"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadKey(path, "password")
	if err == nil {
		t.Error("expected error for invalid hex salt, got nil")
	}
}

func TestLoadKey_InvalidHexNonce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_nonce.json")
	data := `{"pub_key":"aabb","salt":"aabbccdd","nonce":"ZZZZ","cipher_text":"aabb"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadKey(path, "password")
	if err == nil {
		t.Error("expected error for invalid hex nonce, got nil")
	}
}

func TestLoadKey_InvalidHexCipherText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_ct.json")
	data := `{"pub_key":"aabb","salt":"aabbccdd","nonce":"aabbccdd","cipher_text":"ZZZZ"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := LoadKey(path, "password")
	if err == nil {
		t.Error("expected error for invalid hex ciphertext, got nil")
	}
}

func TestSaveKey_InvalidPath(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	badPath := filepath.Join(t.TempDir(), "nonexist", "sub", "key.json")
	err = SaveKey(badPath, "password", priv)
	if err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

func TestNewTx_UnmarshalablePayload(t *testing.T) {
	w, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// math.Inf(1) cannot be marshaled to JSON
	_, err = w.NewTx("chain", "transfer", 0, 5, map[string]any{
		"value": math.Inf(1),
	})
	if err == nil {
		t.Error("expected error for unmarshalable payload, got nil")
	}
}
