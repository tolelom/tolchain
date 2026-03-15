package storage_test

import (
	"fmt"
	"testing"

	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/storage"
)

func setupPrunerDB(t *testing.T, numBlocks int) *testutil.MemDB {
	t.Helper()
	db := testutil.NewMemDB()
	for h := 0; h < numBlocks; h++ {
		hash := fmt.Sprintf("hash_%d", h)
		_ = db.Set([]byte(fmt.Sprintf("height:%d", h)), []byte(hash))
		_ = db.Set([]byte("block:"+hash), []byte(fmt.Sprintf(`{"height":%d}`, h)))
	}
	return db
}

func TestPrune_Disabled(t *testing.T) {
	db := setupPrunerDB(t, 10)
	p := storage.NewPruner(db, 0)

	if err := p.Prune(10); err != nil {
		t.Fatal(err)
	}

	// All blocks should still exist.
	for h := 0; h < 10; h++ {
		if _, err := db.Get([]byte(fmt.Sprintf("height:%d", h))); err != nil {
			t.Errorf("height %d should still exist", h)
		}
	}
}

func TestPrune_KeepsRecentBlocks(t *testing.T) {
	db := setupPrunerDB(t, 20)
	p := storage.NewPruner(db, 5)

	if err := p.Prune(20); err != nil {
		t.Fatal(err)
	}

	// Genesis (height 0) should always be preserved.
	if _, err := db.Get([]byte("height:0")); err != nil {
		t.Error("genesis block should be preserved")
	}

	// Blocks 1-14 should be pruned.
	for h := 1; h < 15; h++ {
		if _, err := db.Get([]byte(fmt.Sprintf("height:%d", h))); err == nil {
			t.Errorf("height %d should be pruned", h)
		}
		hash := fmt.Sprintf("hash_%d", h)
		if _, err := db.Get([]byte("block:" + hash)); err == nil {
			t.Errorf("block %s should be pruned", hash)
		}
	}

	// Blocks 15-19 should still exist.
	for h := 15; h < 20; h++ {
		if _, err := db.Get([]byte(fmt.Sprintf("height:%d", h))); err != nil {
			t.Errorf("height %d should still exist", h)
		}
	}
}

func TestPrune_Idempotent(t *testing.T) {
	db := setupPrunerDB(t, 20)
	p := storage.NewPruner(db, 5)

	// Prune twice at same height.
	if err := p.Prune(20); err != nil {
		t.Fatal(err)
	}
	if err := p.Prune(20); err != nil {
		t.Fatal(err)
	}

	// Recent blocks should still exist.
	for h := 15; h < 20; h++ {
		if _, err := db.Get([]byte(fmt.Sprintf("height:%d", h))); err != nil {
			t.Errorf("height %d should still exist after double prune", h)
		}
	}
}

func TestPrune_IncrementalProgress(t *testing.T) {
	db := setupPrunerDB(t, 30)
	p := storage.NewPruner(db, 10)

	// Prune at height 20: should prune 1-9.
	if err := p.Prune(20); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Get([]byte("height:5")); err == nil {
		t.Error("height 5 should be pruned after first prune")
	}

	// Add more blocks and prune at height 30: should prune 10-19.
	for h := 20; h < 30; h++ {
		hash := fmt.Sprintf("hash_%d", h)
		_ = db.Set([]byte(fmt.Sprintf("height:%d", h)), []byte(hash))
		_ = db.Set([]byte("block:"+hash), []byte("{}"))
	}
	if err := p.Prune(30); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Get([]byte("height:15")); err == nil {
		t.Error("height 15 should be pruned after second prune")
	}
	if _, err := db.Get([]byte("height:25")); err != nil {
		t.Error("height 25 should still exist")
	}
}

func TestPrune_NotEnoughBlocks(t *testing.T) {
	db := setupPrunerDB(t, 5)
	p := storage.NewPruner(db, 10)

	// Current height (5) < keepBlocks (10), no pruning.
	if err := p.Prune(5); err != nil {
		t.Fatal(err)
	}

	for h := 0; h < 5; h++ {
		if _, err := db.Get([]byte(fmt.Sprintf("height:%d", h))); err != nil {
			t.Errorf("height %d should still exist (not enough blocks to prune)", h)
		}
	}
}
