package tests

import (
	"encoding/json"
	"testing"

	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/internal/testutil"
)

func TestPagination(t *testing.T) {
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)

	// Simulate 5 asset mints via events
	for i := 0; i < 5; i++ {
		emitter.Emit(events.Event{
			Type: events.EventAssetMinted,
			Data: map[string]any{
				"owner":    "owner1",
				"asset_id": "asset-" + string(rune('a'+i)),
			},
		})
	}

	// Full list
	all, err := idx.GetAssetsByOwner("owner1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 {
		t.Fatalf("total: got %d want 5", len(all))
	}

	// Page 1: offset=0, limit=2
	result, err := idx.GetAssetsByOwnerPaginated("owner1", 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.IDs) != 2 {
		t.Errorf("page 1 count: got %d want 2", len(result.IDs))
	}
	if result.Total != 5 {
		t.Errorf("total: got %d want 5", result.Total)
	}

	// Page 3: offset=4, limit=2 — should get 1 item
	result, err = idx.GetAssetsByOwnerPaginated("owner1", 4, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.IDs) != 1 {
		t.Errorf("last page count: got %d want 1", len(result.IDs))
	}

	// Offset beyond range
	result, err = idx.GetAssetsByOwnerPaginated("owner1", 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.IDs) != 0 {
		t.Errorf("beyond range: got %d want 0", len(result.IDs))
	}

	// JSON serialization check
	data, _ := json.Marshal(result)
	var decoded indexer.PaginatedResult
	_ = json.Unmarshal(data, &decoded)
	if decoded.Total != 5 {
		t.Errorf("decoded total: got %d want 5", decoded.Total)
	}
}
