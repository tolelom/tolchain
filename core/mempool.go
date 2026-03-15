package core

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/tolelom/tolchain/metrics"
)

const (
	maxMempoolSize = 10_000
	// maxTxAge is the maximum age of a transaction in the mempool (1 hour in nanoseconds).
	maxTxAge       = int64(time.Hour)
	// maxTxFuture is the maximum time a transaction timestamp can be in the future (5 minutes in nanoseconds).
	maxTxFuture    = int64(5 * time.Minute)
)


// Mempool is a thread-safe pending-transaction pool.
type Mempool struct {
	mu        sync.RWMutex
	txs       map[string]*Transaction
	ord       []string                     // insertion-ordered IDs for deterministic pending iteration
	nonces    map[string]map[uint64]string // sender → nonce → tx ID (prevents duplicate nonces)
	maxSize   int
	maxTxAge  int64         // nanoseconds
	maxFuture int64         // nanoseconds
}

// NewMempool creates an empty mempool with the given limits.
// maxSize is the maximum number of transactions in the pool.
// maxTxAgeSec is the maximum past timestamp drift in seconds.
// maxFutureSec is the maximum future timestamp drift in seconds.
func NewMempool(maxSize int, maxTxAgeSec int, maxFutureSec int) *Mempool {
	return &Mempool{
		txs:       make(map[string]*Transaction),
		nonces:    make(map[string]map[uint64]string),
		maxSize:   maxSize,
		maxTxAge:  int64(maxTxAgeSec) * int64(time.Second),
		maxFuture: int64(maxFutureSec) * int64(time.Second),
	}
}

// Add validates and inserts a transaction. Returns an error if the pool is
// full, the tx is already present, the signature is invalid, or the timestamp
// is out of the acceptable window (±1 h / +5 min).
func (m *Mempool) Add(tx *Transaction) error {
	if err := tx.Verify(); err != nil {
		metrics.MempoolRejected.WithLabelValues("invalid_sig").Inc()
		return fmt.Errorf("invalid tx signature: %w", err)
	}
	now := time.Now().UnixNano()
	if now > tx.Timestamp && now-tx.Timestamp > m.maxTxAge {
		metrics.MempoolRejected.WithLabelValues("expired").Inc()
		return errors.New("transaction expired")
	}
	if tx.Timestamp > now && tx.Timestamp-now > m.maxFuture {
		metrics.MempoolRejected.WithLabelValues("future_ts").Inc()
		return errors.New("transaction timestamp too far in the future")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.txs) >= m.maxSize {
		metrics.MempoolRejected.WithLabelValues("full").Inc()
		return errors.New("mempool full")
	}
	if _, exists := m.txs[tx.ID]; exists {
		metrics.MempoolRejected.WithLabelValues("duplicate").Inc()
		return fmt.Errorf("tx already in pool: %w", ErrAlreadyExists)
	}
	// Reject duplicate nonce from the same sender.
	if senderNonces, ok := m.nonces[tx.From]; ok {
		if _, dup := senderNonces[tx.Nonce]; dup {
			metrics.MempoolRejected.WithLabelValues("dup_nonce").Inc()
			return fmt.Errorf("duplicate nonce %d from sender %s", tx.Nonce, tx.From)
		}
	}
	m.txs[tx.ID] = tx
	m.ord = append(m.ord, tx.ID)
	if m.nonces[tx.From] == nil {
		m.nonces[tx.From] = make(map[uint64]string)
	}
	m.nonces[tx.From][tx.Nonce] = tx.ID
	metrics.MempoolAdded.Inc()
	metrics.MempoolSize.Set(float64(len(m.txs)))
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
	actualRemoved := 0
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
			actualRemoved++
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
	// Compact if capacity is more than 2x the length (prevent unbounded growth)
	if cap(m.ord) > 2*len(m.ord)+64 {
		compacted := make([]string, len(m.ord))
		copy(compacted, m.ord)
		m.ord = compacted
	}
	metrics.MempoolRemoved.Add(float64(actualRemoved))
	metrics.MempoolSize.Set(float64(len(m.txs)))
}

// Size returns the current number of pending transactions.
func (m *Mempool) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.txs)
}
