package storage_test

import (
	"fmt"
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/storage"
)

func newTestLevelDB(t *testing.T) *storage.LevelDB {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.NewLevelDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLevelDB_OpenAndClose(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.NewLevelDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestLevelDB_SetAndGet(t *testing.T) {
	db := newTestLevelDB(t)

	if err := db.Set([]byte("key1"), []byte("value1")); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	val, err := db.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if string(val) != "value1" {
		t.Fatalf("expected value1, got %s", val)
	}
}

func TestLevelDB_Get_NonexistentKey(t *testing.T) {
	db := newTestLevelDB(t)

	_, err := db.Get([]byte("nonexistent"))
	if err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLevelDB_Delete(t *testing.T) {
	db := newTestLevelDB(t)

	db.Set([]byte("key1"), []byte("value1"))
	if err := db.Delete([]byte("key1")); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err := db.Get([]byte("key1"))
	if err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestLevelDB_Batch_SetAndWrite(t *testing.T) {
	db := newTestLevelDB(t)

	batch := db.NewBatch()
	batch.Set([]byte("bk1"), []byte("bv1"))
	batch.Set([]byte("bk2"), []byte("bv2"))
	if err := batch.Write(); err != nil {
		t.Fatalf("batch write failed: %v", err)
	}

	val, err := db.Get([]byte("bk1"))
	if err != nil {
		t.Fatalf("get bk1 failed: %v", err)
	}
	if string(val) != "bv1" {
		t.Fatalf("expected bv1, got %s", val)
	}

	val, err = db.Get([]byte("bk2"))
	if err != nil {
		t.Fatalf("get bk2 failed: %v", err)
	}
	if string(val) != "bv2" {
		t.Fatalf("expected bv2, got %s", val)
	}
}

func TestLevelDB_Batch_DeleteAndWrite(t *testing.T) {
	db := newTestLevelDB(t)

	db.Set([]byte("dk1"), []byte("dv1"))

	batch := db.NewBatch()
	batch.Delete([]byte("dk1"))
	if err := batch.Write(); err != nil {
		t.Fatalf("batch write failed: %v", err)
	}

	_, err := db.Get([]byte("dk1"))
	if err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound after batch delete, got %v", err)
	}
}

func TestLevelDB_Batch_Reset(t *testing.T) {
	db := newTestLevelDB(t)

	batch := db.NewBatch()
	batch.Set([]byte("rk1"), []byte("rv1"))
	batch.Reset()
	if err := batch.Write(); err != nil {
		t.Fatalf("batch write after reset failed: %v", err)
	}

	// Key should not exist since batch was reset before write.
	_, err := db.Get([]byte("rk1"))
	if err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound after reset batch write, got %v", err)
	}
}

func TestLevelDB_NewIterator(t *testing.T) {
	db := newTestLevelDB(t)

	db.Set([]byte("prefix:a"), []byte("va"))
	db.Set([]byte("prefix:b"), []byte("vb"))
	db.Set([]byte("other:c"), []byte("vc"))

	it := db.NewIterator([]byte("prefix:"))
	defer it.Release()

	count := 0
	for it.Next() {
		count++
	}
	if err := it.Error(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 items with prefix, got %d", count)
	}
}

// ---- LevelBlockStore tests ----

func newTestBlockStore(t *testing.T) *storage.LevelBlockStore {
	t.Helper()
	db := newTestLevelDB(t)
	return storage.NewLevelBlockStore(db)
}

func TestLevelBlockStore_PutAndGetBlock(t *testing.T) {
	bs := newTestBlockStore(t)

	block := &core.Block{
		Header: core.BlockHeader{Height: 0, ChainID: "test"},
		Hash:   "hash000",
	}
	if err := bs.PutBlock(block); err != nil {
		t.Fatalf("PutBlock failed: %v", err)
	}

	got, err := bs.GetBlock("hash000")
	if err != nil {
		t.Fatalf("GetBlock failed: %v", err)
	}
	if got.Hash != "hash000" {
		t.Fatalf("expected hash hash000, got %s", got.Hash)
	}
	if got.Header.Height != 0 {
		t.Fatalf("expected height 0, got %d", got.Header.Height)
	}
}

func TestLevelBlockStore_GetBlock_NotFound(t *testing.T) {
	bs := newTestBlockStore(t)

	_, err := bs.GetBlock("nonexistent")
	if err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLevelBlockStore_GetBlockByHeight(t *testing.T) {
	bs := newTestBlockStore(t)

	block := &core.Block{
		Header: core.BlockHeader{Height: 5, ChainID: "test"},
		Hash:   "hash005",
	}
	bs.PutBlock(block)
	bs.PutBlockByHeight(5, "hash005")

	got, err := bs.GetBlockByHeight(5)
	if err != nil {
		t.Fatalf("GetBlockByHeight failed: %v", err)
	}
	if got.Hash != "hash005" {
		t.Fatalf("expected hash005, got %s", got.Hash)
	}
}

func TestLevelBlockStore_GetBlockByHeight_NotFound(t *testing.T) {
	bs := newTestBlockStore(t)

	_, err := bs.GetBlockByHeight(99)
	if err != core.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLevelBlockStore_SetTipAndGetTip(t *testing.T) {
	bs := newTestBlockStore(t)

	if err := bs.SetTip("hash001"); err != nil {
		t.Fatalf("SetTip failed: %v", err)
	}
	tip, err := bs.GetTip()
	if err != nil {
		t.Fatalf("GetTip failed: %v", err)
	}
	if tip != "hash001" {
		t.Fatalf("expected tip hash001, got %s", tip)
	}
}

func TestLevelBlockStore_GetTip_FreshStore(t *testing.T) {
	bs := newTestBlockStore(t)

	tip, err := bs.GetTip()
	if err != nil {
		t.Fatalf("GetTip failed: %v", err)
	}
	if tip != "" {
		t.Fatalf("expected empty tip on fresh store, got %q", tip)
	}
}

func TestLevelBlockStore_CommitBlock(t *testing.T) {
	bs := newTestBlockStore(t)

	block := &core.Block{
		Header: core.BlockHeader{Height: 0, ChainID: "test"},
		Hash:   "hash_commit_0",
	}
	if err := bs.CommitBlock(block); err != nil {
		t.Fatalf("CommitBlock failed: %v", err)
	}

	// Verify block is retrievable by hash.
	got, err := bs.GetBlock("hash_commit_0")
	if err != nil {
		t.Fatalf("GetBlock after CommitBlock failed: %v", err)
	}
	if got.Hash != "hash_commit_0" {
		t.Fatalf("expected hash_commit_0, got %s", got.Hash)
	}

	// Verify block is retrievable by height.
	got, err = bs.GetBlockByHeight(0)
	if err != nil {
		t.Fatalf("GetBlockByHeight after CommitBlock failed: %v", err)
	}
	if got.Hash != "hash_commit_0" {
		t.Fatalf("expected hash_commit_0 at height 0, got %s", got.Hash)
	}

	// Verify tip was updated.
	tip, err := bs.GetTip()
	if err != nil {
		t.Fatalf("GetTip after CommitBlock failed: %v", err)
	}
	if tip != "hash_commit_0" {
		t.Fatalf("expected tip hash_commit_0, got %s", tip)
	}
}

func TestLevelBlockStore_CommitBlock_MultipleBlocks(t *testing.T) {
	bs := newTestBlockStore(t)

	block0 := &core.Block{
		Header: core.BlockHeader{Height: 0, ChainID: "test"},
		Hash:   "hash_0",
	}
	block1 := &core.Block{
		Header: core.BlockHeader{Height: 1, ChainID: "test", PrevHash: "hash_0"},
		Hash:   "hash_1",
	}

	if err := bs.CommitBlock(block0); err != nil {
		t.Fatalf("CommitBlock(0) failed: %v", err)
	}
	if err := bs.CommitBlock(block1); err != nil {
		t.Fatalf("CommitBlock(1) failed: %v", err)
	}

	// Tip should be latest block.
	tip, err := bs.GetTip()
	if err != nil {
		t.Fatal(err)
	}
	if tip != "hash_1" {
		t.Fatalf("expected tip hash_1, got %s", tip)
	}

	// Both blocks should be retrievable.
	for _, hash := range []string{"hash_0", "hash_1"} {
		if _, err := bs.GetBlock(hash); err != nil {
			t.Fatalf("GetBlock(%s) failed: %v", hash, err)
		}
	}
}

func TestLevelBlockStore_PutBlockByHeight(t *testing.T) {
	bs := newTestBlockStore(t)

	block := &core.Block{
		Header: core.BlockHeader{Height: 10, ChainID: "test"},
		Hash:   "hash_10",
	}
	if err := bs.PutBlock(block); err != nil {
		t.Fatal(err)
	}
	if err := bs.PutBlockByHeight(10, "hash_10"); err != nil {
		t.Fatal(err)
	}

	got, err := bs.GetBlockByHeight(10)
	if err != nil {
		t.Fatalf("GetBlockByHeight failed: %v", err)
	}
	if got.Header.Height != 10 {
		t.Fatalf("expected height 10, got %d", got.Header.Height)
	}
}

func TestLevelBlockStore_GetBlockRange(t *testing.T) {
	bs := newTestBlockStore(t)

	// Commit 5 blocks (heights 0..4).
	for i := int64(0); i < 5; i++ {
		block := &core.Block{
			Header: core.BlockHeader{Height: i, ChainID: "test"},
			Hash:   fmt.Sprintf("hash_%d", i),
		}
		if err := bs.CommitBlock(block); err != nil {
			t.Fatalf("CommitBlock(%d): %v", i, err)
		}
	}

	// Get range of 3 blocks starting at height 1.
	blocks, err := bs.GetBlockRange(1, 3)
	if err != nil {
		t.Fatalf("GetBlockRange: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	for i, b := range blocks {
		wantHeight := int64(1) + int64(i)
		if b.Header.Height != wantHeight {
			t.Errorf("block[%d] height = %d, want %d", i, b.Header.Height, wantHeight)
		}
		wantHash := fmt.Sprintf("hash_%d", wantHeight)
		if b.Hash != wantHash {
			t.Errorf("block[%d] hash = %q, want %q", i, b.Hash, wantHash)
		}
	}
}

func TestLevelBlockStore_GetBlockRange_StopsAtMissing(t *testing.T) {
	bs := newTestBlockStore(t)

	// Commit 2 blocks (heights 0..1).
	for i := int64(0); i < 2; i++ {
		block := &core.Block{
			Header: core.BlockHeader{Height: i, ChainID: "test"},
			Hash:   fmt.Sprintf("hash_%d", i),
		}
		if err := bs.CommitBlock(block); err != nil {
			t.Fatalf("CommitBlock(%d): %v", i, err)
		}
	}

	// Request 10 blocks but only 2 exist.
	blocks, err := bs.GetBlockRange(0, 10)
	if err != nil {
		t.Fatalf("GetBlockRange: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
}

func TestLevelBlockStore_GetBlockRange_Empty(t *testing.T) {
	bs := newTestBlockStore(t)

	// No blocks committed; range should return empty.
	blocks, err := bs.GetBlockRange(0, 5)
	if err != nil {
		t.Fatalf("GetBlockRange: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
}
