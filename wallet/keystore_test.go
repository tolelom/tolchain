package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tolelom/tolchain/crypto"
)

// --- Generate key pair ---

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if len(priv) == 0 {
		t.Error("private key should not be empty")
	}
	if len(pub) == 0 {
		t.Error("public key should not be empty")
	}
	// Verify the public key is derivable from the private key.
	derived := priv.Public()
	if derived.Hex() != pub.Hex() {
		t.Error("derived public key does not match generated public key")
	}
}

// --- SaveKey/LoadKey round-trip ---

func TestSaveLoadKey_RoundTrip(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	password := "test-password-123"

	if err := SaveKey(path, password, priv); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}

	loaded, err := LoadKey(path, password)
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}

	if loaded.Hex() != priv.Hex() {
		t.Error("loaded private key does not match original")
	}
	if loaded.Public().Hex() != priv.Public().Hex() {
		t.Error("loaded key's public key does not match original")
	}
}

// --- Corrupted nonce must return an error, not panic ---

func TestLoadKey_CorruptedNonceLength(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	password := "pw"

	if err := SaveKey(path, password, priv); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}

	// Truncate the nonce field in the keystore file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ks keystoreFile
	if err := json.Unmarshal(data, &ks); err != nil {
		t.Fatal(err)
	}
	ks.Nonce = "0011" // 2 bytes — far shorter than the GCM nonce size
	corrupted, err := json.Marshal(ks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, corrupted, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadKey(path, password); err == nil {
		t.Fatal("expected error for corrupted nonce, got nil")
	}
}

// --- Decrypted plaintext with wrong key length must return an error ---

func TestLoadKey_WrongKeyLength(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")
	password := "pw"

	// Hand-craft a keystore whose plaintext decrypts fine but is NOT a valid
	// ed25519 private key (wrong length).
	salt := make([]byte, saltSize)
	key := deriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	cipherText := gcm.Seal(nil, nonce, []byte("too-short-key"), nil)

	ks := keystoreFile{
		Salt:       hex.EncodeToString(salt),
		Nonce:      hex.EncodeToString(nonce),
		CipherText: hex.EncodeToString(cipherText),
	}
	data, err := json.Marshal(ks)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadKey(path, password); err == nil {
		t.Fatal("expected error for wrong decrypted key length, got nil")
	}
}

// --- Wrong password returns error ---

func TestLoadKey_WrongPassword(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	if err := SaveKey(path, "correct-password", priv); err != nil {
		t.Fatal(err)
	}

	_, err = LoadKey(path, "wrong-password")
	if err == nil {
		t.Fatal("expected error when loading with wrong password")
	}
}

// --- Corrupted file returns error ---

func TestLoadKey_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted.key")

	// Write garbage data.
	if err := os.WriteFile(path, []byte("not valid json"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadKey(path, "any-password")
	if err == nil {
		t.Error("expected error for corrupted keystore file")
	}
}

func TestLoadKey_CorruptedCipherText(t *testing.T) {
	priv, _, _ := crypto.GenerateKeyPair()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.key")

	if err := SaveKey(path, "password", priv); err != nil {
		t.Fatal(err)
	}

	// Read the file, modify ciphertext, and write back.
	data, _ := os.ReadFile(path)
	// Flip a byte in the ciphertext to corrupt it.
	for i := len(data) - 10; i < len(data)-5; i++ {
		data[i] ^= 0xFF
	}
	_ = os.WriteFile(path, data, 0600)

	_, err := LoadKey(path, "password")
	if err == nil {
		t.Error("expected error for corrupted ciphertext")
	}
}

func TestLoadKey_NonExistentFile(t *testing.T) {
	_, err := LoadKey("/nonexistent/path/to/key.json", "password")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

// --- Multiple save/load cycles with different passwords ---

func TestSaveLoadKey_DifferentPasswords(t *testing.T) {
	priv, _, _ := crypto.GenerateKeyPair()
	dir := t.TempDir()

	passwords := []string{"pass1", "a-longer-password!", ""}
	for i, pw := range passwords {
		path := filepath.Join(dir, "key"+string(rune('0'+i))+".json")
		if err := SaveKey(path, pw, priv); err != nil {
			t.Fatalf("SaveKey with password %q: %v", pw, err)
		}
		loaded, err := LoadKey(path, pw)
		if err != nil {
			t.Fatalf("LoadKey with password %q: %v", pw, err)
		}
		if loaded.Hex() != priv.Hex() {
			t.Errorf("round-trip failed for password %q", pw)
		}
	}
}
