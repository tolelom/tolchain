package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type PrivateKey []byte

type PublicKey []byte

func GenerateKeyPair() (PrivateKey, PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return PrivateKey(priv), PublicKey(pub), nil
}

// Address is the first 20 bytes of SHA-256(pubkey) in hex. On-chain
// accounts are keyed by the full pubkey hex instead; this is for display.
func (pub PublicKey) Address() string {
	h := HashBytes(pub)
	return hex.EncodeToString(h[:20])
}

func (pub PublicKey) Hex() string {
	return hex.EncodeToString(pub)
}

func (priv PrivateKey) Hex() string {
	return hex.EncodeToString(priv)
}

func (priv PrivateKey) Public() PublicKey {
	return PublicKey(ed25519.PrivateKey(priv).Public().(ed25519.PublicKey))
}

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
