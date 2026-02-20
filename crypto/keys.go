package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// PrivateKey wraps ed25519 private key bytes.
type PrivateKey []byte

// PublicKey wraps ed25519 public key bytes.
type PublicKey []byte

// GenerateKeyPair generates a new ed25519 key pair.
func GenerateKeyPair() (PrivateKey, PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return PrivateKey(priv), PublicKey(pub), nil
}

// Address returns a 40-char hex address derived from the public key.
// It takes the first 20 bytes of SHA-256(pubkey).
func (pub PublicKey) Address() string {
	h := HashBytes(pub)
	return hex.EncodeToString(h[:20])
}

// Hex returns the full 64-char hex-encoded public key.
func (pub PublicKey) Hex() string {
	return hex.EncodeToString(pub)
}

// Hex returns the hex-encoded private key.
func (priv PrivateKey) Hex() string {
	return hex.EncodeToString(priv)
}

// Public derives the ed25519 public key from the private key.
func (priv PrivateKey) Public() PublicKey {
	return PublicKey(ed25519.PrivateKey(priv).Public().(ed25519.PublicKey))
}

// PubKeyFromHex decodes a hex-encoded public key.
func PubKeyFromHex(s string) (PublicKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid pubkey hex: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("pubkey must be %d bytes, got %d", ed25519.PublicKeySize, len(b))
	}
	return PublicKey(b), nil
}

// PrivKeyFromHex decodes a hex-encoded private key.
func PrivKeyFromHex(s string) (PrivateKey, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid privkey hex: %w", err)
	}
	if len(b) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("privkey must be %d bytes, got %d", ed25519.PrivateKeySize, len(b))
	}
	return PrivateKey(b), nil
}
