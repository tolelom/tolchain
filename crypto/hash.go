package crypto

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns the SHA-256 hash of data as a lowercase hex string.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// HashBytes returns the raw SHA-256 bytes of data.
func HashBytes(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
