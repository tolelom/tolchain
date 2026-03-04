package core

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	maxMempoolSize = 10_000
	maxTxAge       = int64(time.Hour)          // reject txs older than 1 hour
	maxTxFuture    = int64(5 * time.Minute)    // reject txs more than 5 min in the future
)

// Mempool is a thread-safe pending-transaction pool.
type Mempool struct {
	mu     sync.RWMutex
	txs    map[string]*Transaction
	ord    []string                       // insertion-ordered IDs for deterministic pending iteration
	nonces map[string]map[uint64]string   // sender → nonce → tx ID (prevents duplicate nonces)
}

// NewMempool creates an empty mempool.
func NewMempool() *Mempool {
	return &Mempool{
		txs:    make(map[string]*Transaction),
		nonces: make(map[string]map[uint64]string),
	}
}

// Add validates and inserts a transaction. Returns an error if the pool is
// full, the tx is already present, the signature is invalid, or the timestamp
// is out of the acceptable window (±1 h / +5 min).
func (m *Mempool) Add(tx *Transaction) error {
	if err := tx.Verify(); err != nil {
		return fmt.Errorf("invalid tx signature: %w", err)
	}
	now := time.Now().UnixNano()
	if now > tx.Timestamp && now-tx.Timestamp > maxTxAge {
		return errors.New("transaction expired")
	}
	if tx.Timestamp > now && tx.Timestamp-now > maxTxFuture {
		return errors.New("transaction timestamp too far in the future")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.txs) >= maxMempoolSize {
		return errors.New("mempool full")
	}
	if _, exists := m.txs[tx.ID]; exists {
		return errors.New("tx already in pool")
	}
	// Reject duplicate nonce from the same sender.
	if senderNonces, ok := m.nonces[tx.From]; ok {
		if _, dup := senderNonces[tx.Nonce]; dup {
			return fmt.Errorf("duplicate nonce %d from sender %s", tx.Nonce, tx.From)
		}
	}
	m.txs[tx.ID] = tx
	m.ord = append(m.ord, tx.ID)
	if m.nonces[tx.From] == nil {
		m.nonces[tx.From] = make(map[uint64]string)
	}
	m.nonces[tx.From][tx.Nonce] = tx.ID
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
		if tx, ok := m.txs[id]; ok {
			// Clean up nonce tracking.
			if senderNonces, exists := m.nonces[tx.From]; exists {
				delete(senderNonces, tx.Nonce)
				if len(senderNonces) == 0 {
					delete(m.nonces, tx.From)
				}
			}
			delete(m.txs, id)
		}
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
