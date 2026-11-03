package core

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tolelom/tolchain/crypto"
)

// BlockHeader is what the block hash covers; its JSON encoding is part of
// consensus.
type BlockHeader struct {
	ChainID   string `json:"chain_id"`
	Height    int64  `json:"height"`
	PrevHash  string `json:"prev_hash"`
	StateRoot string `json:"state_root"` // after executing this block
	TxRoot    string `json:"tx_root"`
	// Timestamp is Unix ns; handlers see it as "now". Genesis uses the
	// configured genesis.timestamp instead of the clock.
	Timestamp int64 `json:"timestamp"`
	// Proposer is empty only for the unsigned genesis block.
	Proposer string `json:"proposer"`
}

type Block struct {
	Header       BlockHeader    `json:"header"`
	Transactions []*Transaction `json:"transactions"`
	Hash         string         `json:"hash"`
	Signature    string         `json:"signature"`
}

func (b *Block) ComputeHash() string {
	data, err := json.Marshal(b.Header)
	if err != nil {
		panic("block hash marshal failed: " + err.Error()) // unreachable for these field types
	}
	return crypto.Hash(data)
}

// Sign sets Hash and signs it.
func (b *Block) Sign(priv crypto.PrivateKey) {
	b.Hash = b.ComputeHash()
	b.Signature = crypto.Sign(priv, []byte(b.Hash))
}

// Verify recomputes the hash too, so a header edited after signing fails.
func (b *Block) Verify(pub crypto.PublicKey) error {
	if computed := b.ComputeHash(); b.Hash != computed {
		return fmt.Errorf("block hash mismatch: stored %s computed %s", b.Hash, computed)
	}
	return crypto.Verify(pub, []byte(b.Hash), b.Signature)
}

// VerifyIntegrity checks the hash and TxRoot without the signature.
func (b *Block) VerifyIntegrity() error {
	if computed := b.ComputeHash(); b.Hash != computed {
		return fmt.Errorf("block hash mismatch: stored %s computed %s", b.Hash, computed)
	}
	if txRoot := ComputeTxRoot(b.Transactions); b.Header.TxRoot != txRoot {
		return errors.New("tx_root mismatch")
	}
	return nil
}

// ComputeTxRoot hashes the tx IDs in order, each prefixed with its 4-byte
// big-endian length so different ID lists cannot concatenate to the same bytes.
func ComputeTxRoot(txs []*Transaction) string {
	if len(txs) == 0 {
		return crypto.Hash([]byte("empty"))
	}
	var buf bytes.Buffer
	var lenBuf [4]byte
	for _, tx := range txs {
		id := []byte(tx.ID)
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(id)))
		buf.Write(lenBuf[:])
		buf.Write(id)
	}
	return crypto.Hash(buf.Bytes())
}

// NewBlock returns an unsigned block stamped with the current time.
func NewBlock(chainID string, height int64, prevHash, proposer string, txs []*Transaction) *Block {
	return &Block{
		Header: BlockHeader{
			ChainID:   chainID,
			Height:    height,
			PrevHash:  prevHash,
			TxRoot:    ComputeTxRoot(txs),
			Timestamp: time.Now().UnixNano(),
			Proposer:  proposer,
		},
		Transactions: txs,
	}
}
