// Package indexer maintains secondary indexes over committed blocks so game
// servers can query assets/sessions by owner without scanning full state.
package indexer

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

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
		log.Printf("[indexer] mint index write failed (owner=%s asset=%s): %v", owner, assetID, err)
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
		log.Printf("[indexer] transfer remove failed (from=%s asset=%s): %v", from, assetID, err)
	}
	if err := idx.addToList(prefixOwnerAssets+to, assetID); err != nil {
		log.Printf("[indexer] transfer add failed (to=%s asset=%s): %v", to, assetID, err)
	}
}

func (idx *Indexer) onAssetBurned(ev events.Event) {
	owner, _ := ev.Data["owner"].(string)
	assetID, _ := ev.Data["asset_id"].(string)
	if owner == "" || assetID == "" {
		return
	}
	if err := idx.removeFromList(prefixOwnerAssets+owner, assetID); err != nil {
		log.Printf("[indexer] burn remove failed (owner=%s asset=%s): %v", owner, assetID, err)
	}
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
			if err := idx.addToList(prefixPlayerSession+player, sessionID); err != nil {
				log.Printf("[indexer] session index write failed (player=%s session=%s): %v", player, sessionID, err)
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
		log.Printf("[indexer] market buy remove failed (seller=%s asset=%s): %v", seller, assetID, err)
	}
	if err := idx.addToList(prefixOwnerAssets+buyer, assetID); err != nil {
		log.Printf("[indexer] market buy add failed (buyer=%s asset=%s): %v", buyer, assetID, err)
	}
	if listingID != "" {
		if err := idx.removeFromList(keyActiveListings, listingID); err != nil {
			log.Printf("[indexer] market buy listing remove failed (listing=%s): %v", listingID, err)
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
		log.Printf("[indexer] tx result marshal failed (tx=%s): %v", ev.TxID, err)
		return
	}
	if err := idx.db.Set([]byte(prefixTxResult+ev.TxID), data); err != nil {
		log.Printf("[indexer] tx result write failed (tx=%s): %v", ev.TxID, err)
	}
}

func (idx *Indexer) onMarketList(ev events.Event) {
	listingID, _ := ev.Data["listing_id"].(string)
	if listingID == "" {
		return
	}
	if err := idx.addToList(keyActiveListings, listingID); err != nil {
		log.Printf("[indexer] market list add failed (listing=%s): %v", listingID, err)
	}
}

func (idx *Indexer) onMarketCancel(ev events.Event) {
	listingID, _ := ev.Data["listing_id"].(string)
	if listingID == "" {
		return
	}
	if err := idx.removeFromList(keyActiveListings, listingID); err != nil {
		log.Printf("[indexer] market cancel remove failed (listing=%s): %v", listingID, err)
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
	ids, err := idx.getList(key)
	if err != nil {
		return fmt.Errorf("read list: %w", err)
	}
	for _, id := range ids {
		if id == value {
			return nil // already present
		}
	}
	ids = append(ids, value)
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return idx.db.Set([]byte(key), data)
}

func (idx *Indexer) removeFromList(key, value string) error {
	ids, err := idx.getList(key)
	if err != nil {
		return fmt.Errorf("read list: %w", err)
	}
	if ids == nil {
		return nil
	}
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
