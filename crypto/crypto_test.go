package crypto

import (
	"encoding/hex"
	"strings"
	"testing"
)

// ---- Key generation and encoding ----

func TestGenerateKeyPair(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	// ed25519 private key is 64 bytes, public key is 32 bytes
	if len(priv) != 64 {
		t.Errorf("private key length = %d, want 64", len(priv))
	}
	if len(pub) != 32 {
		t.Errorf("public key length = %d, want 32", len(pub))
	}
}

func TestPubKeyHexRoundtrip(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	hexStr := pub.Hex()
	if len(hexStr) != 64 {
		t.Fatalf("pub.Hex() length = %d, want 64", len(hexStr))
	}
	restored, err := PubKeyFromHex(hexStr)
	if err != nil {
		t.Fatalf("PubKeyFromHex: %v", err)
	}
	if restored.Hex() != hexStr {
		t.Errorf("roundtrip mismatch: got %s, want %s", restored.Hex(), hexStr)
	}
}

func TestPrivKeyHexRoundtrip(t *testing.T) {
	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	hexStr := priv.Hex()
	if len(hexStr) != 128 {
		t.Fatalf("priv.Hex() length = %d, want 128", len(hexStr))
	}
	restored, err := PrivKeyFromHex(hexStr)
	if err != nil {
		t.Fatalf("PrivKeyFromHex: %v", err)
	}
	if restored.Hex() != hexStr {
		t.Errorf("roundtrip mismatch: got %s, want %s", restored.Hex(), hexStr)
	}
}

func TestPubKeyFromHex_InvalidHex(t *testing.T) {
	_, err := PubKeyFromHex("zzzz")
	if err == nil {
		t.Error("expected error for invalid hex, got nil")
	}
}

func TestPubKeyFromHex_WrongLength(t *testing.T) {
	// 16 bytes instead of 32
	short := strings.Repeat("ab", 16)
	_, err := PubKeyFromHex(short)
	if err == nil {
		t.Error("expected error for wrong length, got nil")
	}
}

func TestPrivKeyFromHex_InvalidHex(t *testing.T) {
	_, err := PrivKeyFromHex("not-hex!")
	if err == nil {
		t.Error("expected error for invalid hex, got nil")
	}
}

func TestPrivKeyFromHex_WrongLength(t *testing.T) {
	// 32 bytes instead of 64
	short := strings.Repeat("ab", 32)
	_, err := PrivKeyFromHex(short)
	if err == nil {
		t.Error("expected error for wrong length, got nil")
	}
}

func TestPrivateKey_Public(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	derived := priv.Public()
	if derived.Hex() != pub.Hex() {
		t.Errorf("priv.Public().Hex() = %s, want %s", derived.Hex(), pub.Hex())
	}
}

func TestPublicKey_Address(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	addr := pub.Address()
	if len(addr) != 40 {
		t.Errorf("address length = %d, want 40", len(addr))
	}
	// Must be valid hex
	if _, err := hex.DecodeString(addr); err != nil {
		t.Errorf("address is not valid hex: %v", err)
	}
}

// ---- Signature ----

func TestSignAndVerify(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	data := []byte("hello tolchain")
	sig := Sign(priv, data)
	if err := Verify(pub, data, sig); err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
}

func TestVerify_WrongData(t *testing.T) {
	priv, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	sig := Sign(priv, []byte("original"))
	if err := Verify(pub, []byte("tampered"), sig); err == nil {
		t.Error("expected Verify to fail with wrong data, got nil")
	}
}

func TestVerify_WrongKey(t *testing.T) {
	priv, _, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	_, otherPub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	data := []byte("test data")
	sig := Sign(priv, data)
	if err := Verify(otherPub, data, sig); err == nil {
		t.Error("expected Verify to fail with wrong key, got nil")
	}
}

func TestVerify_InvalidHexSignature(t *testing.T) {
	_, pub, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if err := Verify(pub, []byte("data"), "not-valid-hex!!"); err == nil {
		t.Error("expected Verify to fail with invalid hex signature, got nil")
	}
}

// ---- Hash ----

func TestHash_Deterministic(t *testing.T) {
	data := []byte("deterministic input")
	h1 := Hash(data)
	h2 := Hash(data)
	if h1 != h2 {
		t.Errorf("Hash not deterministic: %s != %s", h1, h2)
	}
}

func TestHash_DifferentInputs(t *testing.T) {
	h1 := Hash([]byte("input A"))
	h2 := Hash([]byte("input B"))
	if h1 == h2 {
		t.Error("Hash produced same output for different inputs")
	}
}

func TestHashBytes_Length(t *testing.T) {
	b := HashBytes([]byte("some data"))
	if len(b) != 32 {
		t.Errorf("HashBytes length = %d, want 32", len(b))
	}
}

func TestHash_MatchesHashBytes(t *testing.T) {
	data := []byte("consistency check")
	hexHash := Hash(data)
	rawHash := HashBytes(data)
	if hexHash != hex.EncodeToString(rawHash) {
		t.Error("Hash and HashBytes are inconsistent")
	}
}
