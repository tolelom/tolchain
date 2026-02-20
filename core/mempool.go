package core

import (
	"errors"
	"fmt"
	"sync"
)

const maxMempoolSize = 10_000

// Mempool is a thread-safe pending-transaction pool.
type Mempool struct {
	mu  sync.RWMutex
	txs map[string]*Transaction
	ord []string // insertion-ordered IDs for deterministic pending iteration
}

// NewMempool creates an empty mempool.
func NewMempool() *Mempool {
	return &Mempool{txs: make(map[string]*Transaction)}
}

// Add validates and inserts a transaction. Returns an error if the pool is
// full, the tx is already present, or the signature is invalid.
func (m *Mempool) Add(tx *Transaction) error {
	if err := tx.Verify(); err != nil {
		return fmt.Errorf("invalid tx signature: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.txs) >= maxMempoolSize {
		return errors.New("mempool full")
	}
	if _, exists := m.txs[tx.ID]; exists {
		return errors.New("tx already in pool")
	}
	m.txs[tx.ID] = tx
	m.ord = append(m.ord, tx.ID)
	return nil
}

// Get returns a transaction by ID.
func (m *Mempool) Get(id string) (*Transaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tx, ok := m.txs[id]
	return tx, ok
}

// Pending returns up to n pending transactions in insertion order.
func (m *Mempool) Pending(n int) []*Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Transaction, 0, n)
	for _, id := range m.ord {
		if tx, ok := m.txs[id]; ok {
			result = append(result, tx)
			if len(result) >= n {
				break
			}
		}
	}
	return result
}

// Remove deletes transactions by ID (called after block commit).
func (m *Mempool) Remove(ids []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := make(map[string]bool, len(ids))
	for _, id := range ids {
		delete(m.txs, id)
		removed[id] = true
	}
	filtered := m.ord[:0]
	for _, id := range m.ord {
		if !removed[id] {
			filtered = append(filtered, id)
		}
	}
	m.ord = filtered
}

// Size returns the current number of pending transactions.
func (m *Mempool) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.txs)
}
