package crypto

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash is lowercase hex SHA-256; block, tx and state hashes all use it.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func HashBytes(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
