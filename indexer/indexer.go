// Package indexer maintains secondary indexes over committed blocks so game
// servers can query assets/sessions by owner without scanning full state.
package indexer

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/storage"
)

const (
	prefixOwnerAssets   = "idx:owner:asset:"
	prefixPlayerSession = "idx:player:session:"
)

// Indexer subscribes to chain events and updates secondary lookup tables.
type Indexer struct {
	db      storage.DB
	emitter *events.Emitter
}

// New creates an Indexer backed by db and subscribes to relevant events.
func New(db storage.DB, emitter *events.Emitter) *Indexer {
	idx := &Indexer{db: db, emitter: emitter}
	emitter.Subscribe(events.EventAssetMinted, idx.onAssetMinted)
	emitter.Subscribe(events.EventAssetTransfer, idx.onAssetTransferred)
	emitter.Subscribe(events.EventAssetBurned, idx.onAssetBurned)
	emitter.Subscribe(events.EventSessionOpen, idx.onSessionOpen)
	return idx
}

// GetAssetsByOwner returns all asset IDs owned by the given pubkey.
func (idx *Indexer) GetAssetsByOwner(owner string) ([]string, error) {
	return idx.getList(prefixOwnerAssets + owner)
}

// GetSessionsByPlayer returns all session IDs a player participated in.
func (idx *Indexer) GetSessionsByPlayer(player string) ([]string, error) {
	return idx.getList(prefixPlayerSession + player)
}

// ---- event handlers ----

func (idx *Indexer) onAssetMinted(ev events.Event) {
	owner, _ := ev.Data["owner"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	if owner == "" || assetID == "" {
		return
	}
	_ = idx.addToList(prefixOwnerAssets+owner, assetID)
}

func (idx *Indexer) onAssetTransferred(ev events.Event) {
	from, _ := ev.Data["from"].(string)
	to, _ := ev.Data["to"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	if assetID == "" || from == "" || to == "" {
		return
	}
	if err := idx.removeFromList(prefixOwnerAssets+from, assetID); err != nil {
		return
	}
	_ = idx.addToList(prefixOwnerAssets+to, assetID)
}

func (idx *Indexer) onAssetBurned(ev events.Event) {
	owner, _ := ev.Data["owner"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	if owner == "" || assetID == "" {
		return
	}
	_ = idx.removeFromList(prefixOwnerAssets+owner, assetID)
}

func (idx *Indexer) onSessionOpen(ev events.Event) {
	sessionID, _ := ev.Data["session_id"].(string)
	players, _ := ev.Data["players"].([]any)
	if sessionID == "" {
		return
	}
	for _, p := range players {
		player, _ := p.(string)
		if player != "" {
			_ = idx.addToList(prefixPlayerSession+player, sessionID)
		}
	}
}

// ---- list helpers ----

func (idx *Indexer) getList(key string) ([]string, error) {
	data, err := idx.db.Get([]byte(key))
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, nil // empty list
		}
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("indexer unmarshal: %w", err)
	}
	return ids, nil
}

func (idx *Indexer) addToList(key, value string) error {
	ids, _ := idx.getList(key)
	ids = append(ids, value)
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return idx.db.Set([]byte(key), data)
}

func (idx *Indexer) removeFromList(key, value string) error {
	ids, _ := idx.getList(key)
	filtered := ids[:0]
	for _, id := range ids {
		if id != value {
			filtered = append(filtered, id)
		}
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return err
	}
	return idx.db.Set([]byte(key), data)
}
