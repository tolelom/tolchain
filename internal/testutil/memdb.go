// Package testutil provides in-memory implementations of storage interfaces
// for use in tests across the module. Never import this in production code.
package testutil

import (
	"strings"
	"sync"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/storage"
)

// MemDB is a thread-safe in-memory storage.DB for tests.
type MemDB struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemDB creates an empty MemDB.
func NewMemDB() *MemDB {
	return &MemDB{data: make(map[string][]byte)}
}

func (m *MemDB) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[string(key)]
	if !ok {
		return nil, core.ErrNotFound
	}
	return v, nil
}

func (m *MemDB) Set(key, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[string(key)] = value
	return nil
}

func (m *MemDB) Delete(key []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, string(key))
	return nil
}

func (m *MemDB) NewIterator(prefix []byte) storage.Iterator {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p := string(prefix)
	var pairs []kv
	for k, v := range m.data {
		if strings.HasPrefix(k, p) {
			cp := make([]byte, len(v))
			copy(cp, v)
			pairs = append(pairs, kv{k: []byte(k), v: cp})
		}
	}
	return &memIter{pairs: pairs, idx: -1}
}

func (m *MemDB) NewBatch() storage.Batch {
	return &memBatch{db: m}
}

func (m *MemDB) Close() error { return nil }

// memBatch is an in-memory atomic write buffer for MemDB.
type memBatch struct {
	db  *MemDB
	ops []memBatchOp
}

type memBatchOp struct {
	key   string
	value []byte // nil means delete
}

func (b *memBatch) Set(key, value []byte) {
	cp := make([]byte, len(value))
	copy(cp, value)
	b.ops = append(b.ops, memBatchOp{string(key), cp})
}

func (b *memBatch) Delete(key []byte) {
	b.ops = append(b.ops, memBatchOp{string(key), nil})
}

func (b *memBatch) Reset() { b.ops = nil }

func (b *memBatch) Write() error {
	b.db.mu.Lock()
	defer b.db.mu.Unlock()
	for _, op := range b.ops {
		if op.value == nil {
			delete(b.db.data, op.key)
		} else {
			b.db.data[op.key] = op.value
		}
	}
	return nil
}

type kv struct{ k, v []byte }

type memIter struct {
	pairs []kv
	idx   int
}

func (it *memIter) Next() bool        { it.idx++; return it.idx < len(it.pairs) }
func (it *memIter) Key() []byte       { return it.pairs[it.idx].k }
func (it *memIter) Value() []byte     { return it.pairs[it.idx].v }
func (it *memIter) Release()          {}
func (it *memIter) Error() error      { return nil }

// MemBlockStore is an in-memory core.BlockStore for tests.
type MemBlockStore struct {
	mu     sync.RWMutex
	blocks map[string]*core.Block
	byH    map[int64]string
	tip    string
}

// NewMemBlockStore creates an empty MemBlockStore.
func NewMemBlockStore() *MemBlockStore {
	return &MemBlockStore{
		blocks: make(map[string]*core.Block),
		byH:    make(map[int64]string),
	}
}

func (s *MemBlockStore) PutBlock(block *core.Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocks[block.Hash] = block
	return nil
}

func (s *MemBlockStore) GetBlock(hash string) (*core.Block, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blocks[hash]
	if !ok {
		return nil, core.ErrNotFound
	}
	return b, nil
}

func (s *MemBlockStore) PutBlockByHeight(height int64, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byH[height] = hash
	return nil
}

func (s *MemBlockStore) GetBlockByHeight(height int64) (*core.Block, error) {
	s.mu.RLock()
	h, ok := s.byH[height]
	s.mu.RUnlock()
	if !ok {
		return nil, core.ErrNotFound
	}
	return s.GetBlock(h)
}

func (s *MemBlockStore) GetTip() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tip, nil
}

func (s *MemBlockStore) SetTip(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tip = hash
	return nil
}

// NewStateDB returns a storage.StateDB backed by a fresh MemDB.
func NewStateDB() *storage.StateDB {
	return storage.NewStateDB(NewMemDB())
}
