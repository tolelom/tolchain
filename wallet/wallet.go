package wallet

import (
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// Wallet holds a key pair and provides transaction-building helpers.
type Wallet struct {
	priv crypto.PrivateKey
	pub  crypto.PublicKey
}

// New creates a Wallet from an existing private key.
func New(priv crypto.PrivateKey) *Wallet {
	return &Wallet{priv: priv, pub: priv.Public()}
}

// Generate creates a Wallet with a freshly generated key pair.
func Generate() (*Wallet, error) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return New(priv), nil
}

// PrivKey returns the raw private key (handle with care).
func (w *Wallet) PrivKey() crypto.PrivateKey {
	return w.priv
}

// PubKey returns the hex-encoded ed25519 public key (used as "from" address).
func (w *Wallet) PubKey() string {
	return w.pub.Hex()
}

// Address returns the short human-readable address (first 20 bytes of SHA-256(pubkey)).
func (w *Wallet) Address() string {
	return w.pub.Address()
}

// NewTx creates a signed transaction. nonce should match the account's current nonce.
func (w *Wallet) NewTx(typ core.TxType, nonce, fee uint64, payload any) (*core.Transaction, error) {
	tx, err := core.NewTransaction(typ, w.pub.Hex(), nonce, fee, payload)
	if err != nil {
		return nil, err
	}
	tx.Sign(w.priv)
	return tx, nil
}

// Transfer creates a signed transfer transaction.
func (w *Wallet) Transfer(to string, amount, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(core.TxTransfer, nonce, fee, core.TransferPayload{
		To:     to,
		Amount: amount,
	})
}
