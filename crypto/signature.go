package crypto

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
)

// Sign signs data with the private key and returns a hex-encoded signature.
func Sign(priv PrivateKey, data []byte) string {
	sig := ed25519.Sign(ed25519.PrivateKey(priv), data)
	return hex.EncodeToString(sig)
}

// Verify checks a hex-encoded signature against data using the public key.
func Verify(pub PublicKey, data []byte, sigHex string) error {
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("invalid signature hex: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), data, sig) {
		return errors.New("signature verification failed")
	}
	return nil
}
