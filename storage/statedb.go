package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

const (
	prefixAccount  = "acct:"
	prefixAsset    = "asset:"
	prefixTemplate = "tmpl:"
	prefixSession  = "sess:"
	prefixListing  = "list:"
)

type stateSnapshot struct {
	dirty   map[string][]byte
	deleted map[string]bool
}

// StateDB implements core.State on top of a DB with in-memory write buffer,
// snapshot/rollback, and deterministic state-root computation.
type StateDB struct {
	db        DB
	dirty     map[string][]byte
	deleted   map[string]bool
	snapshots []stateSnapshot
}

// NewStateDB creates a StateDB backed by db.
func NewStateDB(db DB) *StateDB {
	return &StateDB{
		db:      db,
		dirty:   make(map[string][]byte),
		deleted: make(map[string]bool),
	}
}

// ---- internal helpers ----

func (s *StateDB) get(key string) ([]byte, error) {
	if s.deleted[key] {
		return nil, core.ErrNotFound
	}
	if v, ok := s.dirty[key]; ok {
		return v, nil
	}
	return s.db.Get([]byte(key))
}

func (s *StateDB) set(key string, val []byte) {
	delete(s.deleted, key)
	s.dirty[key] = val
}

func (s *StateDB) del(key string) {
	delete(s.dirty, key)
	s.deleted[key] = true
}

// ---- Account ----

func (s *StateDB) GetAccount(address string) (*core.Account, error) {
	data, err := s.get(prefixAccount + address)
	if errors.Is(err, core.ErrNotFound) {
		return &core.Account{Address: address}, nil // zero-value account
	}
	if err != nil {
		return nil, err
	}
	var acc core.Account
	if err := json.Unmarshal(data, &acc); err != nil {
		return nil, err
	}
	return &acc, nil
}

func (s *StateDB) SetAccount(acc *core.Account) error {
	data, err := json.Marshal(acc)
	if err != nil {
		return err
	}
	s.set(prefixAccount+acc.Address, data)
	return nil
}

// ---- Asset ----

func (s *StateDB) GetAsset(id string) (*core.Asset, error) {
	data, err := s.get(prefixAsset + id)
	if err != nil {
		return nil, err
	}
	var asset core.Asset
	if err := json.Unmarshal(data, &asset); err != nil {
		return nil, err
	}
	return &asset, nil
}

func (s *StateDB) SetAsset(asset *core.Asset) error {
	data, err := json.Marshal(asset)
	if err != nil {
		return err
	}
	s.set(prefixAsset+asset.ID, data)
	return nil
}

func (s *StateDB) DeleteAsset(id string) error {
	s.del(prefixAsset + id)
	return nil
}

// ---- Template ----

func (s *StateDB) GetTemplate(id string) (*core.AssetTemplate, error) {
	data, err := s.get(prefixTemplate + id)
	if err != nil {
		return nil, err
	}
	var t core.AssetTemplate
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *StateDB) SetTemplate(t *core.AssetTemplate) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	s.set(prefixTemplate+t.ID, data)
	return nil
}

// ---- Session ----

func (s *StateDB) GetSession(id string) (*core.Session, error) {
	data, err := s.get(prefixSession + id)
	if err != nil {
		return nil, err
	}
	var sess core.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *StateDB) SetSession(sess *core.Session) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	s.set(prefixSession+sess.ID, data)
	return nil
}

// ---- Market ----

func (s *StateDB) GetListing(id string) (*core.MarketListing, error) {
	data, err := s.get(prefixListing + id)
	if err != nil {
		return nil, err
	}
	var l core.MarketListing
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *StateDB) SetListing(l *core.MarketListing) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	s.set(prefixListing+l.ID, data)
	return nil
}

// ---- Snapshot / Rollback / Commit ----

// Snapshot saves the current write buffer and returns a snapshot ID.
func (s *StateDB) Snapshot() (int, error) {
	snap := stateSnapshot{
		dirty:   make(map[string][]byte, len(s.dirty)),
		deleted: make(map[string]bool, len(s.deleted)),
	}
	for k, v := range s.dirty {
		cp := make([]byte, len(v))
		copy(cp, v)
		snap.dirty[k] = cp
	}
	for k, v := range s.deleted {
		snap.deleted[k] = v
	}
	s.snapshots = append(s.snapshots, snap)
	return len(s.snapshots) - 1, nil
}

// RevertToSnapshot restores the write buffer to a previously saved snapshot.
// The snapshot maps are deep-copied so that subsequent writes cannot corrupt them.
func (s *StateDB) RevertToSnapshot(id int) error {
	if id < 0 || id >= len(s.snapshots) {
		return fmt.Errorf("invalid snapshot id %d", id)
	}
	snap := s.snapshots[id]

	dirty := make(map[string][]byte, len(snap.dirty))
	for k, v := range snap.dirty {
		cp := make([]byte, len(v))
		copy(cp, v)
		dirty[k] = cp
	}
	deleted := make(map[string]bool, len(snap.deleted))
	for k, v := range snap.deleted {
		deleted[k] = v
	}

	s.dirty = dirty
	s.deleted = deleted
	s.snapshots = s.snapshots[:id]
	return nil
}

// Commit flushes the write buffer to the underlying DB and returns a
// deterministic state root (SHA-256 over sorted key-value pairs).
func (s *StateDB) Commit() (string, error) {
	// Flush dirty writes
	for k, v := range s.dirty {
		if err := s.db.Set([]byte(k), v); err != nil {
			return "", err
		}
	}
	// Flush deletions
	for k := range s.deleted {
		if err := s.db.Delete([]byte(k)); err != nil {
			return "", err
		}
	}

	// Build deterministic state root over all touched keys (writes and deletions).
	// Length-prefixed encoding prevents key/value boundary ambiguity.
	// Tombstone marker (0xFFFFFFFF value length) distinguishes deleted from empty.
	seen := make(map[string]bool, len(s.dirty)+len(s.deleted))
	touched := make([]string, 0, len(s.dirty)+len(s.deleted))
	for k := range s.dirty {
		if !seen[k] {
			touched = append(touched, k)
			seen[k] = true
		}
	}
	for k := range s.deleted {
		if !seen[k] {
			touched = append(touched, k)
			seen[k] = true
		}
	}
	sort.Strings(touched)

	var buf bytes.Buffer
	var lenBuf [4]byte
	for _, k := range touched {
		kb := []byte(k)
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(kb)))
		buf.Write(lenBuf[:])
		buf.Write(kb)
		if v, ok := s.dirty[k]; ok {
			binary.BigEndian.PutUint32(lenBuf[:], uint32(len(v)))
			buf.Write(lenBuf[:])
			buf.Write(v)
		} else {
			// tombstone: 0xFFFFFFFF length signals a deletion
			binary.BigEndian.PutUint32(lenBuf[:], 0xFFFFFFFF)
			buf.Write(lenBuf[:])
		}
	}
	root := crypto.Hash(buf.Bytes())

	// Reset write buffer
	s.dirty = make(map[string][]byte)
	s.deleted = make(map[string]bool)
	s.snapshots = nil

	return root, nil
}
