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

type TxResult struct {
	TxID        string `json:"tx_id"`
	BlockHeight int64  `json:"block_height"`
	Success     bool   `json:"success"`
	Error       string `json:"error"`
}

// Indexer keeps lookup lists up to date from committed-block events.
type Indexer struct {
	db storage.DB
}

func New(db storage.DB, emitter *events.Emitter) *Indexer {
	idx := &Indexer{db: db}
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

func (idx *Indexer) GetAssetsByOwner(owner string) ([]string, error) {
	return idx.getList(prefixOwnerAssets + owner)
}

func (idx *Indexer) GetSessionsByPlayer(player string) ([]string, error) {
	return idx.getList(prefixPlayerSession + player)
}

// GetTxResult returns nil, nil for an unknown tx.
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

func (idx *Indexer) GetActiveListings() ([]string, error) {
	return idx.getList(keyActiveListings)
}

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

func (idx *Indexer) GetAssetsByOwnerPaginated(owner string, offset, limit int) (*PaginatedResult, error) {
	return idx.getListPaginated(prefixOwnerAssets+owner, offset, limit)
}

func (idx *Indexer) GetSessionsByPlayerPaginated(player string, offset, limit int) (*PaginatedResult, error) {
	return idx.getListPaginated(prefixPlayerSession+player, offset, limit)
}

func (idx *Indexer) GetActiveListingsPaginated(offset, limit int) (*PaginatedResult, error) {
	return idx.getListPaginated(keyActiveListings, offset, limit)
}

// Event handlers read Data leniently: a field that is missing or of the wrong
// type skips that index update instead of failing the event.

func str(ev events.Event, key string) string {
	v, _ := ev.Data[key].(string)
	return v
}

func (idx *Indexer) onAssetMinted(ev events.Event) {
	owner, assetID := str(ev, "owner"), str(ev, "asset_id")
	if owner == "" || assetID == "" {
		return
	}
	idx.add(prefixOwnerAssets+owner, assetID)
}

func (idx *Indexer) onAssetTransferred(ev events.Event) {
	from, to, assetID := str(ev, "from"), str(ev, "to"), str(ev, "asset_id")
	if assetID == "" || from == "" || to == "" {
		return
	}
	idx.remove(prefixOwnerAssets+from, assetID)
	idx.add(prefixOwnerAssets+to, assetID)
}

func (idx *Indexer) onAssetBurned(ev events.Event) {
	owner, assetID := str(ev, "owner"), str(ev, "asset_id")
	if owner == "" || assetID == "" {
		return
	}
	idx.remove(prefixOwnerAssets+owner, assetID)
}

func (idx *Indexer) onSessionOpen(ev events.Event) {
	sessionID := str(ev, "session_id")
	if sessionID == "" {
		return
	}
	// []string when emitted in-process, []any after a JSON round trip.
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
			idx.add(prefixPlayerSession+player, sessionID)
		}
	}
}

func (idx *Indexer) onMarketBuy(ev events.Event) {
	seller, buyer, assetID, listingID := str(ev, "seller"), str(ev, "buyer"), str(ev, "asset_id"), str(ev, "listing_id")
	if assetID == "" || seller == "" || buyer == "" {
		return
	}
	idx.remove(prefixOwnerAssets+seller, assetID)
	idx.add(prefixOwnerAssets+buyer, assetID)
	if listingID != "" {
		idx.remove(keyActiveListings, listingID)
	}
}

func (idx *Indexer) onTxExecuted(ev events.Event) {
	if ev.TxID == "" {
		return
	}
	success, _ := ev.Data["success"].(bool)
	result := TxResult{
		TxID:        ev.TxID,
		BlockHeight: ev.BlockHeight,
		Success:     success,
		Error:       str(ev, "error"),
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
	if listingID := str(ev, "listing_id"); listingID != "" {
		idx.add(keyActiveListings, listingID)
	}
}

func (idx *Indexer) onMarketCancel(ev events.Event) {
	if listingID := str(ev, "listing_id"); listingID != "" {
		idx.remove(keyActiveListings, listingID)
	}
}

// A list is stored as one key per item, "<list>:<item>" → "1", so adding or
// removing an item never rewrites the whole list.

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

func (idx *Indexer) add(list, item string) {
	if err := idx.db.Set([]byte(list+":"+item), []byte("1")); err != nil {
		slog.Error("index add failed", "component", "indexer", "list", list, "item", item, "error", err)
	}
}

func (idx *Indexer) remove(list, item string) {
	if err := idx.db.Delete([]byte(list + ":" + item)); err != nil {
		slog.Error("index remove failed", "component", "indexer", "list", list, "item", item, "error", err)
	}
}
