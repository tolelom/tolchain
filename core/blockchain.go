package core

import (
	"errors"
	"fmt"
	"sync"
)

// ErrNotFound is returned when a requested object does not exist in storage.
var ErrNotFound = errors.New("not found")

// BlockStore is the persistence interface used by Blockchain.
// Implementations live in the storage package.
type BlockStore interface {
	GetBlock(hash string) (*Block, error)
	PutBlock(block *Block) error
	GetBlockByHeight(height int64) (*Block, error)
	PutBlockByHeight(height int64, hash string) error
	// GetTip returns the current tip hash, or ("", nil) for a fresh chain.
	GetTip() (string, error)
	SetTip(hash string) error
}

// Blockchain manages the canonical chain: stores blocks and tracks the tip.
type Blockchain struct {
	mu     sync.RWMutex
	store  BlockStore
	tip    *Block
	height int64
}

// NewBlockchain returns a Blockchain backed by store.
// Call Init() to load an existing chain tip from storage.
func NewBlockchain(store BlockStore) *Blockchain {
	return &Blockchain{store: store}
}

// Init loads the persisted tip from the block store.
func (bc *Blockchain) Init() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	tipHash, err := bc.store.GetTip()
	if err != nil {
		return fmt.Errorf("get tip: %w", err)
	}
	if tipHash == "" {
		return nil // fresh chain
	}
	tip, err := bc.store.GetBlock(tipHash)
	if err != nil {
		return fmt.Errorf("load tip block: %w", err)
	}
	bc.tip = tip
	bc.height = tip.Header.Height
	return nil
}

// AddBlock persists a block and advances the tip.
func (bc *Blockchain) AddBlock(block *Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if err := bc.store.PutBlock(block); err != nil {
		return fmt.Errorf("put block: %w", err)
	}
	if err := bc.store.PutBlockByHeight(block.Header.Height, block.Hash); err != nil {
		return fmt.Errorf("put block by height: %w", err)
	}
	if err := bc.store.SetTip(block.Hash); err != nil {
		return fmt.Errorf("set tip: %w", err)
	}
	bc.tip = block
	bc.height = block.Header.Height
	return nil
}

// GetBlock returns a block by its hash.
func (bc *Blockchain) GetBlock(hash string) (*Block, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.store.GetBlock(hash)
}

// GetBlockByHeight returns the block at the given height.
func (bc *Blockchain) GetBlockByHeight(height int64) (*Block, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.store.GetBlockByHeight(height)
}

// Tip returns the current chain tip, or nil for a fresh chain.
func (bc *Blockchain) Tip() *Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.tip
}

// Height returns the height of the current tip (0 for a fresh chain).
func (bc *Blockchain) Height() int64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.height
}
