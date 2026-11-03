package core

import (
	"fmt"
	"sync"
)

type BlockStore interface {
	GetBlock(hash string) (*Block, error)
	PutBlock(block *Block) error
	GetBlockByHeight(height int64) (*Block, error)
	PutBlockByHeight(height int64, hash string) error
	// GetBlockRange stops early at the first missing height.
	GetBlockRange(fromHeight int64, limit int) ([]*Block, error)
	// GetTip returns ("", nil) for an empty store.
	GetTip() (string, error)
	SetTip(hash string) error
	// CommitBlock stores the block, its height index and the new tip atomically.
	CommitBlock(block *Block) error
}

// Blockchain is the canonical chain: the block store plus the cached tip.
type Blockchain struct {
	mu     sync.RWMutex
	store  BlockStore
	tip    *Block
	height int64
}

// NewBlockchain needs Init before use to load an existing tip.
func NewBlockchain(store BlockStore) *Blockchain {
	return &Blockchain{store: store}
}

func (bc *Blockchain) Init() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	tipHash, err := bc.store.GetTip()
	if err != nil {
		return fmt.Errorf("get tip: %w", err)
	}
	if tipHash == "" {
		return nil
	}
	tip, err := bc.store.GetBlock(tipHash)
	if err != nil {
		return fmt.Errorf("load tip block: %w", err)
	}
	bc.tip = tip
	bc.height = tip.Header.Height
	return nil
}

// AddBlock accepts only the block that extends the tip (height 0 on an empty
// chain), so a competing block at a known height is never stored.
func (bc *Blockchain) AddBlock(block *Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.tip == nil {
		if block.Header.Height != 0 {
			return fmt.Errorf("first block must be at height 0, got %d", block.Header.Height)
		}
	} else {
		if block.Header.Height <= bc.height {
			return fmt.Errorf("block height %d <= current tip %d (possible fork)", block.Header.Height, bc.height)
		}
		if block.Header.Height != bc.height+1 {
			return fmt.Errorf("block height %d does not follow tip %d", block.Header.Height, bc.height)
		}
		if block.Header.PrevHash != bc.tip.Hash {
			return fmt.Errorf("prev_hash mismatch: got %s want %s", block.Header.PrevHash, bc.tip.Hash)
		}
	}

	if err := bc.store.CommitBlock(block); err != nil {
		return fmt.Errorf("commit block: %w", err)
	}
	bc.tip = block
	bc.height = block.Header.Height
	return nil
}

func (bc *Blockchain) GetBlock(hash string) (*Block, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.store.GetBlock(hash)
}

func (bc *Blockchain) GetBlockByHeight(height int64) (*Block, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.store.GetBlockByHeight(height)
}

func (bc *Blockchain) GetBlockRange(fromHeight int64, limit int) ([]*Block, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.store.GetBlockRange(fromHeight, limit)
}

// Tip is nil for an empty chain.
func (bc *Blockchain) Tip() *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.tip
}

// Height is 0 for an empty chain.
func (bc *Blockchain) Height() int64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.height
}
