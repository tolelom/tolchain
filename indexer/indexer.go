// Package indexer maintains secondary indexes over committed blocks so game
// servers can query assets/sessions by owner without scanning full state.
package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/storage"
)

const (
	prefixOwnerAssets   = "idx:owner:asset:"
	prefixPlayerSession = "idx:player:session:"
	prefixTxResult      = "idx:tx:"
	keyActiveListings   = "idx:market:active"
)

// TxResult stores the outcome of a transaction after execution.
type TxResult struct {
	TxID        string `json:"tx_id"`
	BlockHeight int64  `json:"block_height"`
	Success     bool   `json:"success"`
	Error       string `json:"error"`
}

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
	emitter.Subscribe(events.EventMarketBuy, idx.onMarketBuy)
	emitter.Subscribe(events.EventTxExecuted, idx.onTxExecuted)
	emitter.Subscribe(events.EventMarketList, idx.onMarketList)
	emitter.Subscribe(events.EventMarketCancel, idx.onMarketCancel)
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

// GetTxResult returns the execution result of a transaction, or nil if not found.
func (idx *Indexer) GetTxResult(txID string) (*TxResult, error) {
	data, err := idx.db.Get([]byte(prefixTxResult + txID))
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var result TxResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("indexer unmarshal tx result: %w", err)
	}
	return &result, nil
}

// GetActiveListings returns all currently active market listing IDs.
func (idx *Indexer) GetActiveListings() ([]string, error) {
	return idx.getList(keyActiveListings)
}

// PaginatedResult holds a page of IDs plus total count.
type PaginatedResult struct {
	IDs    []string `json:"ids"`
	Total  int      `json:"total"`
	Offset int      `json:"offset"`
	Limit  int      `json:"limit"`
}

const (
	defaultPageLimit = 50
	maxPageLimit     = 200
)

func clampPagination(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	return offset, limit
}

func (idx *Indexer) getListPaginated(key string, offset, limit int) (*PaginatedResult, error) {
	ids, err := idx.getList(key)
	if err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []string{}
	}
	offset, limit = clampPagination(offset, limit)
	total := len(ids)
	if offset >= total {
		return &PaginatedResult{IDs: []string{}, Total: total, Offset: offset, Limit: limit}, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return &PaginatedResult{IDs: ids[offset:end], Total: total, Offset: offset, Limit: limit}, nil
}

// GetAssetsByOwnerPaginated returns a paginated slice of asset IDs.
func (idx *Indexer) GetAssetsByOwnerPaginated(owner string, offset, limit int) (*PaginatedResult, error) {
	return idx.getListPaginated(prefixOwnerAssets+owner, offset, limit)
}

// GetSessionsByPlayerPaginated returns a paginated slice of session IDs.
func (idx *Indexer) GetSessionsByPlayerPaginated(player string, offset, limit int) (*PaginatedResult, error) {
	return idx.getListPaginated(prefixPlayerSession+player, offset, limit)
}

// GetActiveListingsPaginated returns a paginated slice of active listing IDs.
func (idx *Indexer) GetActiveListingsPaginated(offset, limit int) (*PaginatedResult, error) {
	return idx.getListPaginated(keyActiveListings, offset, limit)
}

// ---- event handlers ----

func (idx *Indexer) onAssetMinted(ev events.Event) {
	owner, _ := ev.Data["owner"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	if owner == "" || assetID == "" {
		return
	}
	if err := idx.addToList(prefixOwnerAssets+owner, assetID); err != nil {
		slog.Error("mint index write failed", "component", "indexer", "owner", owner, "asset_id", assetID, "error", err)
	}
}

func (idx *Indexer) onAssetTransferred(ev events.Event) {
	from, _ := ev.Data["from"].(string)
	to, _ := ev.Data["to"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	if assetID == "" || from == "" || to == "" {
		return
	}
	if err := idx.removeFromList(prefixOwnerAssets+from, assetID); err != nil {
		slog.Error("transfer remove failed", "component", "indexer", "from", from, "asset_id", assetID, "error", err)
	}
	if err := idx.addToList(prefixOwnerAssets+to, assetID); err != nil {
		slog.Error("transfer add failed", "component", "indexer", "to", to, "asset_id", assetID, "error", err)
	}
}

func (idx *Indexer) onAssetBurned(ev events.Event) {
	owner, _ := ev.Data["owner"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	if owner == "" || assetID == "" {
		return
	}
	if err := idx.removeFromList(prefixOwnerAssets+owner, assetID); err != nil {
		slog.Error("burn remove failed", "component", "indexer", "owner", owner, "asset_id", assetID, "error", err)
	}
}

func (idx *Indexer) onSessionOpen(ev events.Event) {
	sessionID, _ := ev.Data["session_id"].(string)
	if sessionID == "" {
		return
	}
	// ev.Data["players"] may be []string (from direct Go calls) or []any
	// (from JSON round-tripping). Handle both with a type switch.
	var players []string
	switch v := ev.Data["players"].(type) {
	case []string:
		players = v
	case []any:
		for _, p := range v {
			if s, ok := p.(string); ok {
				players = append(players, s)
			}
		}
	}
	for _, player := range players {
		if player != "" {
			if err := idx.addToList(prefixPlayerSession+player, sessionID); err != nil {
				slog.Error("session index write failed", "component", "indexer", "player", player, "session_id", sessionID, "error", err)
			}
		}
	}
}

func (idx *Indexer) onMarketBuy(ev events.Event) {
	seller, _ := ev.Data["seller"].(string)
	buyer, _ := ev.Data["buyer"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	listingID, _ := ev.Data["listing_id"].(string)
	if assetID == "" || seller == "" || buyer == "" {
		return
	}
	if err := idx.removeFromList(prefixOwnerAssets+seller, assetID); err != nil {
		slog.Error("market buy remove failed", "component", "indexer", "seller", seller, "asset_id", assetID, "error", err)
	}
	if err := idx.addToList(prefixOwnerAssets+buyer, assetID); err != nil {
		slog.Error("market buy add failed", "component", "indexer", "buyer", buyer, "asset_id", assetID, "error", err)
	}
	if listingID != "" {
		if err := idx.removeFromList(keyActiveListings, listingID); err != nil {
			slog.Error("market buy listing remove failed", "component", "indexer", "listing_id", listingID, "error", err)
		}
	}
}

func (idx *Indexer) onTxExecuted(ev events.Event) {
	if ev.TxID == "" {
		return
	}
	success, _ := ev.Data["success"].(bool)
	errMsg, _ := ev.Data["error"].(string)
	result := TxResult{
		TxID:        ev.TxID,
		BlockHeight: ev.BlockHeight,
		Success:     success,
		Error:       errMsg,
	}
	data, err := json.Marshal(result)
	if err != nil {
		slog.Error("tx result marshal failed", "component", "indexer", "tx_id", ev.TxID, "error", err)
		return
	}
	if err := idx.db.Set([]byte(prefixTxResult+ev.TxID), data); err != nil {
		slog.Error("tx result write failed", "component", "indexer", "tx_id", ev.TxID, "error", err)
	}
}

func (idx *Indexer) onMarketList(ev events.Event) {
	listingID, _ := ev.Data["listing_id"].(string)
	if listingID == "" {
		return
	}
	if err := idx.addToList(keyActiveListings, listingID); err != nil {
		slog.Error("market list add failed", "component", "indexer", "listing_id", listingID, "error", err)
	}
}

func (idx *Indexer) onMarketCancel(ev events.Event) {
	listingID, _ := ev.Data["listing_id"].(string)
	if listingID == "" {
		return
	}
	if err := idx.removeFromList(keyActiveListings, listingID); err != nil {
		slog.Error("market cancel remove failed", "component", "indexer", "listing_id", listingID, "error", err)
	}
}

// ---- list helpers (prefix-key approach) ----

// getList collects all item IDs stored under the given prefix using an iterator.
// Keys are stored as "<prefix>:<item>" → "1".
func (idx *Indexer) getList(prefix string) ([]string, error) {
	iterPrefix := prefix + ":"
	it := idx.db.NewIterator([]byte(iterPrefix))
	defer it.Release()
	var ids []string
	for it.Next() {
		key := string(it.Key())
		item := strings.TrimPrefix(key, iterPrefix)
		if item != "" {
			ids = append(ids, item)
		}
	}
	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("indexer iterate %s: %w", prefix, err)
	}
	return ids, nil
}

// addToList stores a single key "<prefix>:<item>" → "1".
func (idx *Indexer) addToList(prefix, item string) error {
	return idx.db.Set([]byte(prefix+":"+item), []byte("1"))
}

// removeFromList deletes the key "<prefix>:<item>".
func (idx *Indexer) removeFromList(prefix, item string) error {
	return idx.db.Delete([]byte(prefix + ":" + item))
}
