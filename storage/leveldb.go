package storage

import (
	"encoding/json"
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/util"
	"github.com/tolelom/tolchain/core"
)

// LevelDB implements DB using LevelDB.
type LevelDB struct {
	db *leveldb.DB
}

// NewLevelDB opens (or creates) a LevelDB database at path.
func NewLevelDB(path string) (*LevelDB, error) {
	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
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

func (l *LevelDB) Close() error {
	return l.db.Close()
}

// ---- BlockStore implementation ----

// LevelBlockStore implements core.BlockStore on top of LevelDB.
type LevelBlockStore struct {
	db *LevelDB
}

// NewLevelBlockStore wraps a LevelDB instance as a BlockStore.
func NewLevelBlockStore(db *LevelDB) *LevelBlockStore {
	return &LevelBlockStore{db: db}
}

func (s *LevelBlockStore) PutBlock(block *core.Block) error {
	data, err := json.Marshal(block)
	if err != nil {
		return err
	}
	return s.db.Set([]byte("block:"+block.Hash), data)
}

func (s *LevelBlockStore) GetBlock(hash string) (*core.Block, error) {
	data, err := s.db.Get([]byte("block:" + hash))
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
	key := fmt.Sprintf("height:%d", height)
	return s.db.Set([]byte(key), []byte(hash))
}

func (s *LevelBlockStore) GetBlockByHeight(height int64) (*core.Block, error) {
	key := fmt.Sprintf("height:%d", height)
	hash, err := s.db.Get([]byte(key))
	if err != nil {
		return nil, err
	}
	return s.GetBlock(string(hash))
}

func (s *LevelBlockStore) GetTip() (string, error) {
	val, err := s.db.Get([]byte("chain:tip"))
	if err == core.ErrNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(val), nil
}

func (s *LevelBlockStore) SetTip(hash string) error {
	return s.db.Set([]byte("chain:tip"), []byte(hash))
}
