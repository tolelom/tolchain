// Package wallet provides key management and transaction signing helpers.
package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/tolelom/tolchain/crypto"
	"golang.org/x/crypto/pbkdf2"
)

const (
	// saltSize is the byte length of the random salt for key derivation.
	saltSize = 16
	// pbkdf2Iterations is the number of PBKDF2 rounds for key derivation.
	pbkdf2Iterations = 210_000
	// derivedKeySize is the byte length of the AES key derived from the password.
	derivedKeySize = 32
)

type keystoreFile struct {
	PubKey     string `json:"pub_key"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	CipherText string `json:"cipher_text"`
}

// SaveKey encrypts priv with password and writes it to path.
// Key derivation: PBKDF2-SHA256 with 210k iterations (see deriveKey).
func SaveKey(path, password string, priv crypto.PrivateKey) error {
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	key := deriveKey(password, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	cipherText := gcm.Seal(nil, nonce, priv, nil)

	ks := keystoreFile{
		PubKey:     priv.Public().Hex(),
		Salt:       hex.EncodeToString(salt),
		Nonce:      hex.EncodeToString(nonce),
		CipherText: hex.EncodeToString(cipherText),
	}
	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// LoadKey decrypts the keystore at path using password.
func LoadKey(path, password string) (crypto.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ks keystoreFile
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, err
	}
	salt, err := hex.DecodeString(ks.Salt)
	if err != nil {
		return nil, err
	}
	nonce, err := hex.DecodeString(ks.Nonce)
	if err != nil {
		return nil, err
	}
	cipherText, err := hex.DecodeString(ks.CipherText)
	if err != nil {
		return nil, err
	}

	key := deriveKey(password, salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// gcm.Open panics on a wrong-size nonce; validate it first.
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("corrupted keystore: invalid nonce length")
	}
	privBytes, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return nil, errors.New("wrong password or corrupted keystore")
	}
	if len(privBytes) != ed25519.PrivateKeySize {
		return nil, errors.New("corrupted keystore: invalid private key length")
	}
	return crypto.PrivateKey(privBytes), nil
}

func deriveKey(password string, salt []byte) []byte {
	return pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, derivedKeySize, sha256.New)
}
