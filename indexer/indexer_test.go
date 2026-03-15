package indexer_test

import (
	"fmt"
	"testing"

	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/internal/testutil"
)

func setup() *indexer.Indexer {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	return indexer.New(db, emitter)
}

func setupWithEmitter() (*indexer.Indexer, *events.Emitter) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	return idx, emitter
}

func TestAssetMinted_GetAssetsByOwner(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventAssetMinted,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"owner": "alice", "asset_id": "a1"},
	})

	assets, err := idx.GetAssetsByOwner("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0] != "a1" {
		t.Fatalf("expected [a1], got %v", assets)
	}
}

func TestAssetTransferred_OwnerChanges(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventAssetMinted,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"owner": "alice", "asset_id": "a1"},
	})

	emitter.Emit(events.Event{
		Type:        events.EventAssetTransfer,
		TxID:        "tx2",
		BlockHeight: 2,
		Data:        map[string]any{"from": "alice", "to": "bob", "asset_id": "a1"},
	})

	aliceAssets, err := idx.GetAssetsByOwner("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceAssets) != 0 {
		t.Fatalf("alice should have no assets, got %v", aliceAssets)
	}

	bobAssets, err := idx.GetAssetsByOwner("bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(bobAssets) != 1 || bobAssets[0] != "a1" {
		t.Fatalf("expected bob to have [a1], got %v", bobAssets)
	}
}

func TestAssetBurned_RemovedFromOwner(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventAssetMinted,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"owner": "alice", "asset_id": "a1"},
	})

	emitter.Emit(events.Event{
		Type:        events.EventAssetBurned,
		TxID:        "tx2",
		BlockHeight: 2,
		Data:        map[string]any{"owner": "alice", "asset_id": "a1"},
	})

	assets, err := idx.GetAssetsByOwner("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 0 {
		t.Fatalf("expected empty after burn, got %v", assets)
	}
}

func TestSessionOpen_AllPlayersHaveSession(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventSessionOpen,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"session_id": "s1", "players": []any{"alice", "bob"}},
	})

	for _, player := range []string{"alice", "bob"} {
		sessions, err := idx.GetSessionsByPlayer(player)
		if err != nil {
			t.Fatal(err)
		}
		if len(sessions) != 1 || sessions[0] != "s1" {
			t.Fatalf("expected [s1] for %s, got %v", player, sessions)
		}
	}
}

func TestMarketList_GetActiveListings(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventMarketList,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"listing_id": "l1"},
	})

	listings, err := idx.GetActiveListings()
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 || listings[0] != "l1" {
		t.Fatalf("expected [l1], got %v", listings)
	}
}

func TestMarketBuy_ListingRemovedAndOwnerChanges(t *testing.T) {
	idx, emitter := setupWithEmitter()

	// Mint asset to alice
	emitter.Emit(events.Event{
		Type:        events.EventAssetMinted,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"owner": "alice", "asset_id": "a1"},
	})

	// List it
	emitter.Emit(events.Event{
		Type:        events.EventMarketList,
		TxID:        "tx2",
		BlockHeight: 2,
		Data:        map[string]any{"listing_id": "l1"},
	})

	// Buy it
	emitter.Emit(events.Event{
		Type:        events.EventMarketBuy,
		TxID:        "tx3",
		BlockHeight: 3,
		Data:        map[string]any{"seller": "alice", "buyer": "bob", "asset_id": "a1", "listing_id": "l1"},
	})

	// Listing should be removed from active
	listings, err := idx.GetActiveListings()
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 0 {
		t.Fatalf("expected no active listings, got %v", listings)
	}

	// Asset owner should change
	aliceAssets, err := idx.GetAssetsByOwner("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(aliceAssets) != 0 {
		t.Fatalf("alice should have no assets, got %v", aliceAssets)
	}

	bobAssets, err := idx.GetAssetsByOwner("bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(bobAssets) != 1 || bobAssets[0] != "a1" {
		t.Fatalf("expected bob to have [a1], got %v", bobAssets)
	}
}

func TestMarketCancel_ListingRemoved(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventMarketList,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"listing_id": "l1"},
	})

	emitter.Emit(events.Event{
		Type:        events.EventMarketCancel,
		TxID:        "tx2",
		BlockHeight: 2,
		Data:        map[string]any{"listing_id": "l1"},
	})

	listings, err := idx.GetActiveListings()
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 0 {
		t.Fatalf("expected no active listings after cancel, got %v", listings)
	}
}

func TestTxExecuted_GetTxResult(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventTxExecuted,
		TxID:        "tx1",
		BlockHeight: 5,
		Data:        map[string]any{"success": true, "error": ""},
	})

	result, err := idx.GetTxResult("tx1")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TxID != "tx1" {
		t.Fatalf("expected tx_id tx1, got %s", result.TxID)
	}
	if result.BlockHeight != 5 {
		t.Fatalf("expected block_height 5, got %d", result.BlockHeight)
	}
	if !result.Success {
		t.Fatal("expected success=true")
	}
}

func TestGetTxResult_Nonexistent_ReturnsNil(t *testing.T) {
	idx, _ := setupWithEmitter()

	result, err := idx.GetTxResult("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Fatal("expected nil result for nonexistent tx")
	}
}

func TestPagination_OffsetLimit(t *testing.T) {
	idx, emitter := setupWithEmitter()

	// Add 10 assets to alice
	for i := 0; i < 10; i++ {
		emitter.Emit(events.Event{
			Type:        events.EventAssetMinted,
			TxID:        fmt.Sprintf("tx%d", i),
			BlockHeight: int64(i + 1),
			Data:        map[string]any{"owner": "alice", "asset_id": fmt.Sprintf("a%d", i)},
		})
	}

	// Get page: offset=2, limit=3
	result, err := idx.GetAssetsByOwnerPaginated("alice", 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 10 {
		t.Fatalf("expected total 10, got %d", result.Total)
	}
	if len(result.IDs) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(result.IDs))
	}
	if result.Offset != 2 {
		t.Fatalf("expected offset 2, got %d", result.Offset)
	}
	if result.Limit != 3 {
		t.Fatalf("expected limit 3, got %d", result.Limit)
	}
}

func TestPagination_OffsetBeyondTotal(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventAssetMinted,
		TxID:        "tx1",
		BlockHeight: 1,
		Data:        map[string]any{"owner": "alice", "asset_id": "a1"},
	})

	result, err := idx.GetAssetsByOwnerPaginated("alice", 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.IDs) != 0 {
		t.Fatalf("expected empty IDs for offset beyond total, got %v", result.IDs)
	}
	if result.Total != 1 {
		t.Fatalf("expected total 1, got %d", result.Total)
	}
}

func TestPagination_ClampDefaults(t *testing.T) {
	idx, emitter := setupWithEmitter()

	// Add 60 assets
	for i := 0; i < 60; i++ {
		emitter.Emit(events.Event{
			Type:        events.EventAssetMinted,
			TxID:        fmt.Sprintf("tx%d", i),
			BlockHeight: int64(i + 1),
			Data:        map[string]any{"owner": "alice", "asset_id": fmt.Sprintf("a%d", i)},
		})
	}

	// limit=0 should default to 50
	result, err := idx.GetAssetsByOwnerPaginated("alice", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Limit != 50 {
		t.Fatalf("expected limit clamped to 50, got %d", result.Limit)
	}
	if len(result.IDs) != 50 {
		t.Fatalf("expected 50 IDs, got %d", len(result.IDs))
	}

	// limit > 200 should clamp to 200
	result, err = idx.GetAssetsByOwnerPaginated("alice", 0, 500)
	if err != nil {
		t.Fatal(err)
	}
	if result.Limit != 200 {
		t.Fatalf("expected limit clamped to 200, got %d", result.Limit)
	}

	// offset < 0 should clamp to 0
	result, err = idx.GetAssetsByOwnerPaginated("alice", -5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Offset != 0 {
		t.Fatalf("expected offset clamped to 0, got %d", result.Offset)
	}
}

func TestPagination_SessionsByPlayer(t *testing.T) {
	idx, emitter := setupWithEmitter()

	for i := 0; i < 5; i++ {
		emitter.Emit(events.Event{
			Type:        events.EventSessionOpen,
			TxID:        fmt.Sprintf("tx%d", i),
			BlockHeight: int64(i + 1),
			Data:        map[string]any{"session_id": fmt.Sprintf("s%d", i), "players": []any{"alice"}},
		})
	}

	result, err := idx.GetSessionsByPlayerPaginated("alice", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 5 {
		t.Fatalf("expected total 5, got %d", result.Total)
	}
	if len(result.IDs) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(result.IDs))
	}
}

func TestPagination_ActiveListings(t *testing.T) {
	idx, emitter := setupWithEmitter()

	for i := 0; i < 5; i++ {
		emitter.Emit(events.Event{
			Type:        events.EventMarketList,
			TxID:        fmt.Sprintf("tx%d", i),
			BlockHeight: int64(i + 1),
			Data:        map[string]any{"listing_id": fmt.Sprintf("l%d", i)},
		})
	}

	result, err := idx.GetActiveListingsPaginated(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 5 {
		t.Fatalf("expected total 5, got %d", result.Total)
	}
	if len(result.IDs) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(result.IDs))
	}
}

// ---------- Empty/missing data field tests (early-return guards) ----------

func TestOnAssetMinted_EmptyData(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	// Missing owner
	emitter.Emit(events.Event{Type: events.EventAssetMinted, Data: map[string]any{"asset_id": "a1"}})
	// Missing asset_id
	emitter.Emit(events.Event{Type: events.EventAssetMinted, Data: map[string]any{"owner": "alice"}})
	// Both missing
	emitter.Emit(events.Event{Type: events.EventAssetMinted, Data: map[string]any{}})
	// No crash = pass
}

func TestOnAssetTransferred_EmptyData(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{Type: events.EventAssetTransfer, Data: map[string]any{}})
	emitter.Emit(events.Event{Type: events.EventAssetTransfer, Data: map[string]any{"asset_id": "a1", "from": "alice"}})
	emitter.Emit(events.Event{Type: events.EventAssetTransfer, Data: map[string]any{"asset_id": "a1", "to": "bob"}})
}

func TestOnAssetBurned_EmptyData(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{Type: events.EventAssetBurned, Data: map[string]any{}})
	emitter.Emit(events.Event{Type: events.EventAssetBurned, Data: map[string]any{"owner": "alice"}})
}

func TestOnSessionOpen_EmptyData(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{Type: events.EventSessionOpen, Data: map[string]any{}})
	emitter.Emit(events.Event{Type: events.EventSessionOpen, Data: map[string]any{"session_id": "s1"}}) // no players
	// Players with empty strings
	emitter.Emit(events.Event{Type: events.EventSessionOpen, Data: map[string]any{"session_id": "s2", "players": []any{""}}})
}

func TestOnMarketBuy_EmptyData(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{Type: events.EventMarketBuy, Data: map[string]any{}})
	emitter.Emit(events.Event{Type: events.EventMarketBuy, Data: map[string]any{"asset_id": "a1", "seller": "alice", "buyer": "bob"}}) // no listing_id
}

func TestOnTxExecuted_EmptyTxID(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{Type: events.EventTxExecuted, TxID: "", Data: map[string]any{"success": true, "error": ""}})
}

func TestOnMarketList_EmptyData(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{Type: events.EventMarketList, Data: map[string]any{}})
}

func TestOnMarketCancel_EmptyData(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{Type: events.EventMarketCancel, Data: map[string]any{}})
}

func TestAddToList_Deduplication(t *testing.T) {
	idx, emitter := setupWithEmitter()

	// Add same asset twice
	emitter.Emit(events.Event{Type: events.EventAssetMinted, Data: map[string]any{"owner": "alice", "asset_id": "a1"}})
	emitter.Emit(events.Event{Type: events.EventAssetMinted, Data: map[string]any{"owner": "alice", "asset_id": "a1"}})

	ids, err := idx.GetAssetsByOwner("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 (deduplicated), got %d", len(ids))
	}
}

func TestRemoveFromList_NonexistentItem(t *testing.T) {
	idx, emitter := setupWithEmitter()

	// Add one asset
	emitter.Emit(events.Event{Type: events.EventAssetMinted, Data: map[string]any{"owner": "alice", "asset_id": "a1"}})

	// Burn a different asset (not in list) - should not crash
	emitter.Emit(events.Event{Type: events.EventAssetBurned, Data: map[string]any{"owner": "alice", "asset_id": "a999"}})

	ids, _ := idx.GetAssetsByOwner("alice")
	if len(ids) != 1 {
		t.Fatalf("expected 1, got %d", len(ids))
	}
}

func TestRemoveFromList_EmptyList(t *testing.T) {
	_, emitter := setupWithEmitter()

	// Burn an asset that was never added - should not crash
	emitter.Emit(events.Event{Type: events.EventAssetBurned, Data: map[string]any{"owner": "nobody", "asset_id": "a1"}})
}

// ---------- Corrupt data tests (cover json.Unmarshal error branches) ----------

func TestGetAssetsByOwner_PrefixKeyApproach(t *testing.T) {
	// With prefix-key indexing, each asset is stored as a separate key
	// (idx:owner:asset:alice:asset1 → "1"), so there is no JSON to corrupt.
	// This test verifies that the prefix-key iterator works correctly.
	db := testutil.NewMemDB()
	db.Set([]byte("idx:owner:asset:alice:asset1"), []byte("1"))
	db.Set([]byte("idx:owner:asset:alice:asset2"), []byte("1"))
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)

	assets, err := idx.GetAssetsByOwner("alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
}

func TestGetTxResult_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:tx:tx1"), []byte("{bad json"))
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)

	_, err := idx.GetTxResult("tx1")
	if err == nil {
		t.Fatal("expected error for corrupt tx result data, got nil")
	}
}

func TestGetAssetsByOwnerPaginated_PrefixKey(t *testing.T) {
	db := testutil.NewMemDB()
	for i := 0; i < 5; i++ {
		db.Set([]byte(fmt.Sprintf("idx:owner:asset:alice:a%d", i)), []byte("1"))
	}
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)

	page, err := idx.GetAssetsByOwnerPaginated("alice", 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.IDs) != 3 {
		t.Fatalf("expected 3 assets, got %d", len(page.IDs))
	}
}

func TestGetSessionsByPlayer_PrefixKey(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:player:session:bob:s1"), []byte("1"))
	db.Set([]byte("idx:player:session:bob:s2"), []byte("1"))
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)

	sessions, err := idx.GetSessionsByPlayer("bob")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestGetActiveListings_PrefixKey(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:market:active:listing1"), []byte("1"))
	db.Set([]byte("idx:market:active:listing2"), []byte("1"))
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)

	listings, err := idx.GetActiveListings()
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 2 {
		t.Fatalf("expected 2 listings, got %d", len(listings))
	}
}

func TestOnTxExecuted_FailedTx(t *testing.T) {
	idx, emitter := setupWithEmitter()

	emitter.Emit(events.Event{
		Type:        events.EventTxExecuted,
		TxID:        "fail-tx",
		BlockHeight: 10,
		Data:        map[string]any{"success": false, "error": "insufficient funds"},
	})

	result, err := idx.GetTxResult("fail-tx")
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for failed tx")
	}
	if result.Success {
		t.Fatal("expected success=false")
	}
	if result.Error != "insufficient funds" {
		t.Fatalf("expected error message 'insufficient funds', got %q", result.Error)
	}
}

func TestOnAssetMinted_CorruptExistingList(t *testing.T) {
	db := testutil.NewMemDB()
	// Pre-corrupt the owner's asset list so addToList's internal getList fails.
	db.Set([]byte("idx:owner:asset:alice"), []byte("BAD"))
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	// Minting should hit the slog.Error branch but not panic.
	emitter.Emit(events.Event{
		Type: events.EventAssetMinted,
		Data: map[string]any{"owner": "alice", "asset_id": "a1"},
	})
}

func TestOnAssetBurned_CorruptExistingList(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:owner:asset:alice"), []byte("BAD"))
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	// Burning should hit the slog.Error branch (removeFromList read error) but not panic.
	emitter.Emit(events.Event{
		Type: events.EventAssetBurned,
		Data: map[string]any{"owner": "alice", "asset_id": "a1"},
	})
}

func TestOnAssetTransferred_CorruptExistingList(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:owner:asset:alice"), []byte("BAD"))
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	// Transfer should hit slog.Error branches for both remove and add but not panic.
	emitter.Emit(events.Event{
		Type: events.EventAssetTransfer,
		Data: map[string]any{"from": "alice", "to": "bob", "asset_id": "a1"},
	})
}

func TestOnMarketBuy_CorruptExistingList(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:owner:asset:seller1"), []byte("BAD"))
	db.Set([]byte("idx:market:active"), []byte("BAD"))
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{
		Type: events.EventMarketBuy,
		Data: map[string]any{"seller": "seller1", "buyer": "buyer1", "asset_id": "a1", "listing_id": "l1"},
	})
}

func TestOnMarketList_CorruptExistingList(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:market:active"), []byte("BAD"))
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{
		Type: events.EventMarketList,
		Data: map[string]any{"listing_id": "l1"},
	})
}

func TestOnMarketCancel_CorruptExistingList(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:market:active"), []byte("BAD"))
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{
		Type: events.EventMarketCancel,
		Data: map[string]any{"listing_id": "l1"},
	})
}

func TestOnSessionOpen_CorruptExistingList(t *testing.T) {
	db := testutil.NewMemDB()
	db.Set([]byte("idx:player:session:alice"), []byte("BAD"))
	emitter := events.NewEmitter()
	_ = indexer.New(db, emitter)

	emitter.Emit(events.Event{
		Type: events.EventSessionOpen,
		Data: map[string]any{"session_id": "s1", "players": []any{"alice"}},
	})
}
