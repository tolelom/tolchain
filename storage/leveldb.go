package storage

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/tolelom/tolchain/core"
)

// Block store key layout, shared with the pruner and offline tools.
const (
	BlockKeyPrefix = "block:"
	ChainTipKey    = "chain:tip"
)

func blockKey(hash string) []byte { return []byte(BlockKeyPrefix + hash) }

func heightKey(height int64) []byte { return []byte(fmt.Sprintf("height:%d", height)) }

type LevelDB struct {
	db *leveldb.DB
}

// NewLevelDB opens or creates the database at path, running goleveldb's
// RecoverFile when the manifest is corrupted.
func NewLevelDB(path string) (*LevelDB, error) {
	db, err := leveldb.OpenFile(path, nil)
	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		slog.Warn("corrupted database detected, attempting recovery", "path", path)
		db, err = leveldb.RecoverFile(path, nil)
		if err != nil {
			return nil, fmt.Errorf("recover leveldb %q: %w", path, err)
		}
		slog.Info("database recovery successful", "path", path)
	} else if err != nil {
		return nil, fmt.Errorf("open leveldb %q: %w", path, err)
	}
	return &LevelDB{db: db}, nil
}

func (l *LevelDB) Get(key []byte) ([]byte, error) {
	val, err := l.db.Get(key, nil)
	if err == leveldb.ErrNotFound {
		return nil, core.ErrNotFound
	}
	return val, err
}

func (l *LevelDB) Set(key, value []byte) error {
	return l.db.Put(key, value, nil)
}

func (l *LevelDB) Delete(key []byte) error {
	return l.db.Delete(key, nil)
}

func (l *LevelDB) NewIterator(prefix []byte) Iterator {
	return l.db.NewIterator(util.BytesPrefix(prefix), nil)
}

func (l *LevelDB) NewBatch() Batch {
	return &levelBatch{db: l.db, b: new(leveldb.Batch)}
}

func (l *LevelDB) Close() error {
	return l.db.Close()
}

type levelBatch struct {
	db *leveldb.DB
	b  *leveldb.Batch
}

func (lb *levelBatch) Set(key, value []byte) { lb.b.Put(key, value) }
func (lb *levelBatch) Delete(key []byte)     { lb.b.Delete(key) }
func (lb *levelBatch) Reset()                { lb.b.Reset() }

// Write syncs to disk: a committed block must survive a crash.
func (lb *levelBatch) Write() error {
	return lb.db.Write(lb.b, &opt.WriteOptions{Sync: true})
}

type LevelBlockStore struct {
	db *LevelDB
}

func NewLevelBlockStore(db *LevelDB) *LevelBlockStore {
	return &LevelBlockStore{db: db}
}

func (s *LevelBlockStore) PutBlock(block *core.Block) error {
	data, err := json.Marshal(block)
	if err != nil {
		return err
	}
	return s.db.Set(blockKey(block.Hash), data)
}

func (s *LevelBlockStore) GetBlock(hash string) (*core.Block, error) {
	data, err := s.db.Get(blockKey(hash))
	if err != nil {
		return nil, err
	}
	var b core.Block
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *LevelBlockStore) PutBlockByHeight(height int64, hash string) error {
	return s.db.Set(heightKey(height), []byte(hash))
}

func (s *LevelBlockStore) GetBlockByHeight(height int64) (*core.Block, error) {
	hash, err := s.db.Get(heightKey(height))
	if err != nil {
		return nil, err
	}
	return s.GetBlock(string(hash))
}

// GetBlockRange stops at the first missing height (end of chain).
func (s *LevelBlockStore) GetBlockRange(fromHeight int64, limit int) ([]*core.Block, error) {
	blocks := make([]*core.Block, 0, limit)
	for h := fromHeight; h < fromHeight+int64(limit); h++ {
		b, err := s.GetBlockByHeight(h)
		if err != nil {
			if stderrors.Is(err, core.ErrNotFound) {
				break
			}
			return nil, fmt.Errorf("get block at height %d: %w", h, err)
		}
		blocks = append(blocks, b)
	}
	return blocks, nil
}

func (s *LevelBlockStore) GetTip() (string, error) {
	val, err := s.db.Get([]byte(ChainTipKey))
	if stderrors.Is(err, core.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(val), nil
}

func (s *LevelBlockStore) SetTip(hash string) error {
	return s.db.Set([]byte(ChainTipKey), []byte(hash))
}

// CommitBlock writes the block, its height index and the tip in one batch so a
// crash cannot leave them inconsistent.
func (s *LevelBlockStore) CommitBlock(block *core.Block) error {
	data, err := json.Marshal(block)
	if err != nil {
		return err
	}
	batch := s.db.NewBatch()
	batch.Set(blockKey(block.Hash), data)
	batch.Set(heightKey(block.Header.Height), []byte(block.Hash))
	batch.Set([]byte(ChainTipKey), []byte(block.Hash))
	return batch.Write()
}
