package core_test

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/internal/testutil"
)

// bcTestKeys generates a key pair for blockchain tests.
func bcTestKeys(t *testing.T) (crypto.PrivateKey, crypto.PublicKey, string) {
	t.Helper()
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return priv, pub, pub.Hex()
}

// bcMakeBlock creates a signed block at the given height with the given prevHash.
func bcMakeBlock(t *testing.T, priv crypto.PrivateKey, pubHex string, height int64, prevHash string) *core.Block {
	t.Helper()
	block := core.NewBlock("test", height, prevHash, pubHex, nil)
	block.Header.StateRoot = "dummy"
	block.Sign(priv)
	return block
}

func TestBlockchain_FreshState(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	if bc.Tip() != nil {
		t.Error("fresh blockchain Tip() should be nil")
	}
	if bc.Height() != 0 {
		t.Errorf("fresh blockchain Height() = %d, want 0", bc.Height())
	}
}

func TestBlockchain_InitEmptyStore(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	if err := bc.Init(); err != nil {
		t.Fatalf("Init on empty store should not error: %v", err)
	}
	if bc.Tip() != nil {
		t.Error("Tip should still be nil after Init on empty store")
	}
}

func TestBlockchain_AddBlockGenesis(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")

	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis should succeed: %v", err)
	}
	if bc.Tip() == nil {
		t.Fatal("Tip should not be nil after adding genesis")
	}
	if bc.Tip().Hash != genesis.Hash {
		t.Errorf("Tip hash = %q, want %q", bc.Tip().Hash, genesis.Hash)
	}
	if bc.Height() != 0 {
		t.Errorf("Height = %d, want 0", bc.Height())
	}
}

func TestBlockchain_AddBlockNonZeroFirstFails(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	block := bcMakeBlock(t, priv, pubHex, 1, "")

	if err := bc.AddBlock(block); err == nil {
		t.Error("AddBlock with height 1 as first block should fail")
	}
}

func TestBlockchain_SequentialAddBlock(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}

	block1 := bcMakeBlock(t, priv, pubHex, 1, genesis.Hash)
	if err := bc.AddBlock(block1); err != nil {
		t.Fatalf("AddBlock height 1: %v", err)
	}

	block2 := bcMakeBlock(t, priv, pubHex, 2, block1.Hash)
	if err := bc.AddBlock(block2); err != nil {
		t.Fatalf("AddBlock height 2: %v", err)
	}

	if bc.Height() != 2 {
		t.Errorf("Height = %d, want 2", bc.Height())
	}
	if bc.Tip().Hash != block2.Hash {
		t.Errorf("Tip hash = %q, want %q", bc.Tip().Hash, block2.Hash)
	}
}

func TestBlockchain_AddBlockWrongHeight(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}

	// Try to add a block at height 3 (skipping 1 and 2).
	block := bcMakeBlock(t, priv, pubHex, 3, genesis.Hash)
	if err := bc.AddBlock(block); err == nil {
		t.Error("AddBlock with wrong height should fail")
	}
}

func TestBlockchain_AddBlockWrongPrevHash(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}

	// Correct height but wrong PrevHash.
	block := bcMakeBlock(t, priv, pubHex, 1, "wrong_prev_hash")
	if err := bc.AddBlock(block); err == nil {
		t.Error("AddBlock with wrong PrevHash should fail")
	}
}

func TestBlockchain_AddBlockAtOrBelowCurrentHeight(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}

	block1 := bcMakeBlock(t, priv, pubHex, 1, genesis.Hash)
	if err := bc.AddBlock(block1); err != nil {
		t.Fatalf("AddBlock height 1: %v", err)
	}

	// Try to add another block at height 0 (fork).
	fork0 := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(fork0); err == nil {
		t.Error("AddBlock at height 0 when tip is 1 should fail (fork rejection)")
	}

	// Try to add another block at height 1 (same height as tip).
	fork1 := bcMakeBlock(t, priv, pubHex, 1, genesis.Hash)
	if err := bc.AddBlock(fork1); err == nil {
		t.Error("AddBlock at height 1 when tip is 1 should fail (fork rejection)")
	}
}

func TestBlockchain_GetBlockByHash(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}

	got, err := bc.GetBlock(genesis.Hash)
	if err != nil {
		t.Fatalf("GetBlock: %v", err)
	}
	if got.Hash != genesis.Hash {
		t.Errorf("GetBlock returned block with hash %q, want %q", got.Hash, genesis.Hash)
	}
}

func TestBlockchain_GetBlockByHeight(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}

	block1 := bcMakeBlock(t, priv, pubHex, 1, genesis.Hash)
	if err := bc.AddBlock(block1); err != nil {
		t.Fatalf("AddBlock height 1: %v", err)
	}

	got, err := bc.GetBlockByHeight(1)
	if err != nil {
		t.Fatalf("GetBlockByHeight(1): %v", err)
	}
	if got.Hash != block1.Hash {
		t.Errorf("GetBlockByHeight(1) hash = %q, want %q", got.Hash, block1.Hash)
	}
}

func TestBlockchain_TipAndHeightUpdateCorrectly(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	// Before any blocks.
	if bc.Tip() != nil {
		t.Error("Tip should be nil before any blocks")
	}
	if bc.Height() != 0 {
		t.Errorf("Height should be 0 before any blocks, got %d", bc.Height())
	}

	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}
	if bc.Tip().Hash != genesis.Hash {
		t.Errorf("Tip after genesis = %q, want %q", bc.Tip().Hash, genesis.Hash)
	}
	if bc.Height() != 0 {
		t.Errorf("Height after genesis = %d, want 0", bc.Height())
	}

	block1 := bcMakeBlock(t, priv, pubHex, 1, genesis.Hash)
	if err := bc.AddBlock(block1); err != nil {
		t.Fatalf("AddBlock height 1: %v", err)
	}
	if bc.Tip().Hash != block1.Hash {
		t.Errorf("Tip after block 1 = %q, want %q", bc.Tip().Hash, block1.Hash)
	}
	if bc.Height() != 1 {
		t.Errorf("Height after block 1 = %d, want 1", bc.Height())
	}

	block2 := bcMakeBlock(t, priv, pubHex, 2, block1.Hash)
	if err := bc.AddBlock(block2); err != nil {
		t.Fatalf("AddBlock height 2: %v", err)
	}
	if bc.Tip().Hash != block2.Hash {
		t.Errorf("Tip after block 2 = %q, want %q", bc.Tip().Hash, block2.Hash)
	}
	if bc.Height() != 2 {
		t.Errorf("Height after block 2 = %d, want 2", bc.Height())
	}
}

func TestBlockchain_InitLoadsExistingTip(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()

	// Build a chain with the first blockchain instance.
	bc1 := core.NewBlockchain(store)
	genesis := bcMakeBlock(t, priv, pubHex, 0, "")
	if err := bc1.AddBlock(genesis); err != nil {
		t.Fatalf("AddBlock genesis: %v", err)
	}
	block1 := bcMakeBlock(t, priv, pubHex, 1, genesis.Hash)
	if err := bc1.AddBlock(block1); err != nil {
		t.Fatalf("AddBlock height 1: %v", err)
	}

	// Create a second blockchain instance from the same store and Init.
	bc2 := core.NewBlockchain(store)
	if err := bc2.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if bc2.Tip() == nil {
		t.Fatal("Tip should not be nil after Init with existing data")
	}
	if bc2.Tip().Hash != block1.Hash {
		t.Errorf("Tip hash = %q, want %q", bc2.Tip().Hash, block1.Hash)
	}
	if bc2.Height() != 1 {
		t.Errorf("Height = %d, want 1", bc2.Height())
	}
}

func TestBlockchain_GetBlockRange(t *testing.T) {
	priv, _, pubHex := bcTestKeys(t)
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)

	// Build a chain of 5 blocks (heights 0..4).
	prevHash := ""
	hashes := make([]string, 5)
	for i := int64(0); i < 5; i++ {
		block := bcMakeBlock(t, priv, pubHex, i, prevHash)
		if err := bc.AddBlock(block); err != nil {
			t.Fatalf("AddBlock height %d: %v", i, err)
		}
		hashes[i] = block.Hash
		prevHash = block.Hash
	}

	// Get range starting at height 1, limit 3.
	blocks, err := bc.GetBlockRange(1, 3)
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
		if b.Hash != hashes[wantHeight] {
			t.Errorf("block[%d] hash = %q, want %q", i, b.Hash, hashes[wantHeight])
		}
	}

	// Request beyond chain end -- should return only available blocks.
	blocks, err = bc.GetBlockRange(3, 10)
	if err != nil {
		t.Fatalf("GetBlockRange beyond end: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
}
