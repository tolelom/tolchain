package core

import (
	"encoding/json"
	"time"

	"github.com/tolelom/tolchain/crypto"
)

// BlockHeader contains the block metadata that is hashed and signed.
type BlockHeader struct {
	Height    int64  `json:"height"`
	PrevHash  string `json:"prev_hash"`
	StateRoot string `json:"state_root"` // hash of state after executing this block
	TxRoot    string `json:"tx_root"`    // hash of all transaction IDs
	Timestamp int64  `json:"timestamp"`
	Proposer  string `json:"proposer"` // proposer's pubkey hex
}

// Block is a collection of transactions with a signed header.
type Block struct {
	Header       BlockHeader    `json:"header"`
	Transactions []*Transaction `json:"transactions"`
	Hash         string         `json:"hash"`
	Signature    string         `json:"signature"`
}

// ComputeHash returns the SHA-256 hash of the serialised header.
// Returns an empty string if marshalling fails (which cannot happen in practice).
func (b *Block) ComputeHash() string {
	data, err := json.Marshal(b.Header)
	if err != nil {
		return ""
	}
	return crypto.Hash(data)
}

// Sign sets Hash and signs the block with the proposer's private key.
func (b *Block) Sign(priv crypto.PrivateKey) {
	b.Hash = b.ComputeHash()
	b.Signature = crypto.Sign(priv, []byte(b.Hash))
}

// Verify checks the block signature against the given public key.
func (b *Block) Verify(pub crypto.PublicKey) error {
	return crypto.Verify(pub, []byte(b.Hash), b.Signature)
}

// ComputeTxRoot builds a deterministic root hash from all transaction IDs.
func ComputeTxRoot(txs []*Transaction) string {
	if len(txs) == 0 {
		return crypto.Hash([]byte("empty"))
	}
	var ids []byte
	for _, tx := range txs {
		ids = append(ids, []byte(tx.ID)...)
	}
	return crypto.Hash(ids)
}

// NewBlock creates an unsigned block with the given parameters.
func NewBlock(height int64, prevHash, proposer string, txs []*Transaction) *Block {
	return &Block{
		Header: BlockHeader{
			Height:    height,
			PrevHash:  prevHash,
			TxRoot:    ComputeTxRoot(txs),
			Timestamp: time.Now().UnixNano(),
			Proposer:  proposer,
		},
		Transactions: txs,
	}
}
