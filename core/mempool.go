package core

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tolelom/tolchain/metrics"
)

// Mempool holds verified txs waiting for a block.
type Mempool struct {
	mu        sync.RWMutex
	txs       map[string]*Transaction
	ord       []string                     // IDs in arrival order
	nonces    map[string]map[uint64]string // NonceAccount → nonce → tx ID
	maxSize   int
	maxTxAge  int64 // ns
	maxFuture int64 // ns
}

// NewMempool bounds the pool size and how far a tx timestamp may lie in the
// past or future.
func NewMempool(maxSize int, maxTxAgeSec int, maxFutureSec int) *Mempool {
	return &Mempool{
		txs:       make(map[string]*Transaction),
		nonces:    make(map[string]map[uint64]string),
		maxSize:   maxSize,
		maxTxAge:  int64(maxTxAgeSec) * int64(time.Second),
		maxFuture: int64(maxFutureSec) * int64(time.Second),
	}
}

// Add rejects a tx that fails verification, is outside the timestamp window,
// is already pooled, reuses a pooled nonce of its account, or finds the pool
// full.
func (m *Mempool) Add(tx *Transaction) error {
	// Verified before taking the lock; duplicates are caught under it.
	if err := tx.Verify(); err != nil {
		metrics.MempoolRejected.WithLabelValues("invalid_sig").Inc()
		return fmt.Errorf("invalid tx signature: %w", err)
	}
	now := time.Now().UnixNano()
	if m.expired(tx, now) {
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
	account := tx.NonceAccount()
	if _, dup := m.nonces[account][tx.Nonce]; dup {
		metrics.MempoolRejected.WithLabelValues("dup_nonce").Inc()
		return fmt.Errorf("duplicate nonce %d from sender %s", tx.Nonce, account)
	}
	m.txs[tx.ID] = tx
	m.ord = append(m.ord, tx.ID)
	if m.nonces[account] == nil {
		m.nonces[account] = make(map[uint64]string)
	}
	m.nonces[account][tx.Nonce] = tx.ID
	metrics.MempoolAdded.Inc()
	metrics.MempoolSize.Set(float64(len(m.txs)))
	return nil
}

func (m *Mempool) expired(tx *Transaction, now int64) bool {
	return now > tx.Timestamp && now-tx.Timestamp > m.maxTxAge
}

func (m *Mempool) Get(id string) (*Transaction, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	tx, ok := m.txs[id]
	return tx, ok
}

// Pending returns up to n unexpired txs in arrival order, except that each
// account's txs are reordered by nonce among the positions they occupy.
func (m *Mempool) Pending(n int) []*Transaction {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now().UnixNano()
	live := make([]*Transaction, 0, len(m.ord))
	for _, id := range m.ord {
		if tx, ok := m.txs[id]; ok && !m.expired(tx, now) {
			live = append(live, tx)
		}
	}
	sortEachAccountByNonce(live)
	if len(live) > n {
		live = live[:n]
	}
	return live
}

func sortEachAccountByNonce(txs []*Transaction) {
	slots := make(map[string][]int)
	for i, tx := range txs {
		account := tx.NonceAccount()
		slots[account] = append(slots[account], i)
	}
	for _, idx := range slots {
		group := make([]*Transaction, len(idx))
		for j, i := range idx {
			group[j] = txs[i]
		}
		sort.SliceStable(group, func(a, b int) bool { return group[a].Nonce < group[b].Nonce })
		for j, i := range idx {
			txs[i] = group[j]
		}
	}
}

// NonceAccount is the account whose nonce and fee the tx spends.
func (tx *Transaction) NonceAccount() string {
	if tx.OnBehalfOf != "" {
		return tx.OnBehalfOf
	}
	return tx.From
}

// EvictExpired drops expired txs and returns how many were dropped.
func (m *Mempool) EvictExpired() int {
	m.mu.RLock()
	now := time.Now().UnixNano()
	var expired []string
	for id, tx := range m.txs {
		if m.expired(tx, now) {
			expired = append(expired, id)
		}
	}
	m.mu.RUnlock()
	if len(expired) == 0 {
		return 0
	}
	m.Remove(expired)
	return len(expired)
}

func (m *Mempool) Remove(ids []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := make(map[string]bool, len(ids))
	actualRemoved := 0
	for _, id := range ids {
		if tx, ok := m.txs[id]; ok {
			account := tx.NonceAccount()
			if accountNonces, exists := m.nonces[account]; exists {
				delete(accountNonces, tx.Nonce)
				if len(accountNonces) == 0 {
					delete(m.nonces, account)
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
	// Reallocate so a once-full pool does not pin its peak capacity.
	if cap(m.ord) > 2*len(m.ord)+64 {
		compacted := make([]string, len(m.ord))
		copy(compacted, m.ord)
		m.ord = compacted
	}
	metrics.MempoolRemoved.Add(float64(actualRemoved))
	metrics.MempoolSize.Set(float64(len(m.txs)))
}

func (m *Mempool) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.txs)
}
