package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// registerPrefix is the only way to declare a state prefix, so ComputeRoot
// and backups can never miss one.
func registerPrefix(p string) string {
	statePrefixes = append(statePrefixes, p)
	return p
}

var statePrefixes []string

var (
	prefixAccount      = registerPrefix("acct:")
	prefixAsset        = registerPrefix("asset:")
	prefixTemplate     = registerPrefix("tmpl:")
	prefixSession      = registerPrefix("sess:")
	prefixListing      = registerPrefix("list:")
	prefixInventory    = registerPrefix("inv:")
	prefixRandomCommit = registerPrefix("rcom:")
	prefixDelegation   = registerPrefix("deleg:")
)

// StatePrefixes returns a copy of every prefix that holds world state.
func StatePrefixes() []string {
	out := make([]string, len(statePrefixes))
	copy(out, statePrefixes)
	return out
}

func delegationKey(granter, grantee string) string {
	return prefixDelegation + granter + ":" + grantee
}

type journalEntry struct {
	key        string
	prevValue  []byte
	hadValue   bool
	wasDeleted bool
}

// StateDB implements core.State over a DB with an in-memory write buffer that
// is flushed only by Commit.
//
// mu guards the buffer. blockMu is separate: block execution holds it for
// writing across the whole execute-commit cycle and RPC reads hold it for
// reading, so queries never see a half-applied block.
type StateDB struct {
	mu        sync.Mutex
	blockMu   sync.RWMutex
	db        DB
	dirty     map[string][]byte
	deleted   map[string]bool
	journal   []journalEntry
	snapshots []int
}

func NewStateDB(db DB) *StateDB {
	return &StateDB{
		db:      db,
		dirty:   make(map[string][]byte),
		deleted: make(map[string]bool),
	}
}

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
	s.record(key)
	delete(s.deleted, key)
	s.dirty[key] = val
}

func (s *StateDB) del(key string) {
	s.record(key)
	delete(s.dirty, key)
	s.deleted[key] = true
}

func (s *StateDB) record(key string) {
	if len(s.snapshots) == 0 {
		return
	}
	prev, had := s.dirty[key]
	s.journal = append(s.journal, journalEntry{key: key, prevValue: prev, hadValue: had, wasDeleted: s.deleted[key]})
}

func getRecord[T any](s *StateDB, key string) (*T, error) {
	s.mu.Lock()
	data, err := s.get(key)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *StateDB) putRecord(key string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set(key, data)
	return nil
}

func (s *StateDB) deleteRecord(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.del(key)
	return nil
}

// GetAccount returns a zero-balance account for an unknown address.
func (s *StateDB) GetAccount(address string) (*core.Account, error) {
	acc, err := getRecord[core.Account](s, prefixAccount+address)
	if errors.Is(err, core.ErrNotFound) {
		return &core.Account{Address: address}, nil
	}
	return acc, err
}

func (s *StateDB) SetAccount(acc *core.Account) error {
	return s.putRecord(prefixAccount+acc.Address, acc)
}

func (s *StateDB) GetAsset(id string) (*core.Asset, error) {
	return getRecord[core.Asset](s, prefixAsset+id)
}

func (s *StateDB) SetAsset(asset *core.Asset) error {
	return s.putRecord(prefixAsset+asset.ID, asset)
}

func (s *StateDB) DeleteAsset(id string) error {
	return s.deleteRecord(prefixAsset + id)
}

func (s *StateDB) GetTemplate(id string) (*core.AssetTemplate, error) {
	return getRecord[core.AssetTemplate](s, prefixTemplate+id)
}

func (s *StateDB) SetTemplate(t *core.AssetTemplate) error {
	return s.putRecord(prefixTemplate+t.ID, t)
}

func (s *StateDB) GetSession(id string) (*core.Session, error) {
	return getRecord[core.Session](s, prefixSession+id)
}

func (s *StateDB) SetSession(sess *core.Session) error {
	return s.putRecord(prefixSession+sess.ID, sess)
}

func (s *StateDB) GetListing(id string) (*core.MarketListing, error) {
	return getRecord[core.MarketListing](s, prefixListing+id)
}

func (s *StateDB) SetListing(l *core.MarketListing) error {
	return s.putRecord(prefixListing+l.ID, l)
}

// GetInventory returns an empty inventory for an owner who never equipped.
func (s *StateDB) GetInventory(owner string) (*core.Inventory, error) {
	inv, err := getRecord[core.Inventory](s, prefixInventory+owner)
	if errors.Is(err, core.ErrNotFound) {
		return &core.Inventory{Owner: owner, Slots: make(map[string]string)}, nil
	}
	return inv, err
}

func (s *StateDB) SetInventory(inv *core.Inventory) error {
	return s.putRecord(prefixInventory+inv.Owner, inv)
}

func (s *StateDB) GetRandomCommitment(id string) (*core.RandomCommitment, error) {
	return getRecord[core.RandomCommitment](s, prefixRandomCommit+id)
}

func (s *StateDB) SetRandomCommitment(rc *core.RandomCommitment) error {
	return s.putRecord(prefixRandomCommit+rc.ID, rc)
}

func (s *StateDB) GetDelegation(granter, grantee string) (*core.DelegationGrant, error) {
	return getRecord[core.DelegationGrant](s, delegationKey(granter, grantee))
}

func (s *StateDB) SetDelegation(grant *core.DelegationGrant) error {
	return s.putRecord(delegationKey(grant.Granter, grant.Grantee), grant)
}

func (s *StateDB) DeleteDelegation(granter, grantee string) error {
	return s.deleteRecord(delegationKey(granter, grantee))
}

func (s *StateDB) GetDelegationsByGranter(granter string) ([]*core.DelegationGrant, error) {
	return s.delegations(prefixDelegation+granter+":", func(*core.DelegationGrant) bool { return true })
}

// GetDelegationsByGrantee scans every grant: keys are ordered by granter.
func (s *StateDB) GetDelegationsByGrantee(grantee string) ([]*core.DelegationGrant, error) {
	return s.delegations(prefixDelegation, func(g *core.DelegationGrant) bool { return g.Grantee == grantee })
}

func (s *StateDB) delegations(prefix string, keep func(*core.DelegationGrant) bool) ([]*core.DelegationGrant, error) {
	s.mu.Lock()
	entries, err := s.visibleEntries(prefix)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	grants := make([]*core.DelegationGrant, 0, len(entries))
	for _, v := range entries {
		var g core.DelegationGrant
		if err := json.Unmarshal(v, &g); err != nil {
			continue
		}
		if keep(&g) {
			grants = append(grants, &g)
		}
	}
	return grants, nil
}

// visibleEntries returns persisted entries under prefixes overlaid with the
// write buffer. Caller must hold s.mu.
func (s *StateDB) visibleEntries(prefixes ...string) (map[string][]byte, error) {
	merged := make(map[string][]byte)
	for _, prefix := range prefixes {
		it := s.db.NewIterator([]byte(prefix))
		for it.Next() {
			merged[string(it.Key())] = bytes.Clone(it.Value())
		}
		it.Release()
		if err := it.Error(); err != nil {
			return nil, fmt.Errorf("iterate state prefix %q: %w", prefix, err)
		}
	}
	for k, v := range s.dirty {
		if hasAnyPrefix(k, prefixes) {
			merged[k] = v
		}
	}
	for k := range s.deleted {
		delete(merged, k)
	}
	return merged, nil
}

func hasAnyPrefix(key string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func (s *StateDB) BlockRLock()   { s.blockMu.RLock() }
func (s *StateDB) BlockRUnlock() { s.blockMu.RUnlock() }
func (s *StateDB) BlockLock()    { s.blockMu.Lock() }
func (s *StateDB) BlockUnlock()  { s.blockMu.Unlock() }

func (s *StateDB) Snapshot() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, len(s.journal))
	return len(s.snapshots) - 1, nil
}

// RevertToSnapshot undoes every write since snapshot id and drops id and all
// later snapshots.
func (s *StateDB) RevertToSnapshot(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id < 0 || id >= len(s.snapshots) {
		return fmt.Errorf("invalid snapshot id %d", id)
	}
	mark := s.snapshots[id]
	for i := len(s.journal) - 1; i >= mark; i-- {
		e := s.journal[i]
		if e.hadValue {
			s.dirty[e.key] = e.prevValue
		} else {
			delete(s.dirty, e.key)
		}
		if e.wasDeleted {
			s.deleted[e.key] = true
		} else {
			delete(s.deleted, e.key)
		}
	}
	s.journal = s.journal[:mark]
	s.snapshots = s.snapshots[:id]
	return nil
}

// ComputeRoot hashes the whole world state including the write buffer, without
// flushing it. Every key/value under the state prefixes, sorted by key, is
// written as 4-byte big-endian length + bytes for both key and value.
//
// It is O(N) in total state size; an incremental trie is deferred until
// profiling demands it.
func (s *StateDB) ComputeRoot() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	merged, err := s.visibleEntries(statePrefixes...)
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, k := range keys {
		writeLengthPrefixed(&buf, []byte(k))
		writeLengthPrefixed(&buf, merged[k])
	}
	return crypto.Hash(buf.Bytes()), nil
}

func writeLengthPrefixed(buf *bytes.Buffer, b []byte) {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(b)))
	buf.Write(lenBuf[:])
	buf.Write(b)
}

// Commit writes the buffer to the DB in one atomic batch and clears it,
// including all snapshots.
func (s *StateDB) Commit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch := s.db.NewBatch()
	for k, v := range s.dirty {
		batch.Set([]byte(k), v)
	}
	for k := range s.deleted {
		batch.Delete([]byte(k))
	}
	if err := batch.Write(); err != nil {
		return err
	}
	s.dirty = make(map[string][]byte)
	s.deleted = make(map[string]bool)
	s.journal = nil
	s.snapshots = nil
	return nil
}
