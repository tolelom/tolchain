package consensus

import (
	"testing"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"
)

const testChainID = "poa-test-chain"

// setupPoA creates a PoA engine with a single validator and a genesis block.
func setupPoA(t *testing.T) (*PoA, *core.Blockchain, *wallet.Wallet) {
	t.Helper()

	w, err := wallet.Generate()
	if err != nil {
		t.Fatal(err)
	}

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	if err := bc.Init(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		NodeID:      "test",
		MaxBlockTxs: 500,
		Validators:  []string{w.PubKey()},
		Genesis: config.GenesisConfig{
			ChainID: testChainID,
			Alloc:   map[string]uint64{w.PubKey(): 1_000_000},
		},
	}

	genesis, err := config.CreateGenesisBlock(cfg, state, w.PrivKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatal(err)
	}

	emitter := events.NewEmitter()
	mempool := core.NewMempool()
	exec := vm.NewExecutor(state, emitter)
	poa := New(cfg, bc, state, mempool, exec, emitter, w.PrivKey())

	return poa, bc, w
}

// --- IsProposer tests ---

func TestIsProposer_SingleValidator(t *testing.T) {
	poa, _, _ := setupPoA(t)
	if !poa.IsProposer() {
		t.Error("single validator should always be proposer")
	}
}

func TestIsProposer_EmptyValidators(t *testing.T) {
	w, err := wallet.Generate()
	if err != nil {
		t.Fatal(err)
	}

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	_ = bc.Init()

	cfg := &config.Config{
		NodeID:     "test",
		Validators: nil, // empty
		Genesis:    config.GenesisConfig{ChainID: testChainID},
	}

	emitter := events.NewEmitter()
	mempool := core.NewMempool()
	exec := vm.NewExecutor(state, emitter)
	poa := New(cfg, bc, state, mempool, exec, emitter, w.PrivKey())

	if poa.IsProposer() {
		t.Error("should return false when validators list is empty")
	}
}

func TestIsProposer_MultipleValidatorsRoundRobin(t *testing.T) {
	w1, _ := wallet.Generate()
	w2, _ := wallet.Generate()
	w3, _ := wallet.Generate()

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	_ = bc.Init()

	validators := []string{w1.PubKey(), w2.PubKey(), w3.PubKey()}
	cfg := &config.Config{
		NodeID:      "test",
		MaxBlockTxs: 500,
		Validators:  validators,
		Genesis: config.GenesisConfig{
			ChainID: testChainID,
			Alloc:   map[string]uint64{w1.PubKey(): 1_000_000},
		},
	}

	genesis, _ := config.CreateGenesisBlock(cfg, state, w1.PrivKey())
	_ = bc.AddBlock(genesis)

	emitter := events.NewEmitter()
	mempool := core.NewMempool()
	exec := vm.NewExecutor(state, emitter)

	// Genesis is height 0. Next height = 1. idx = 1 % 3 = 1 -> w2.
	poa1 := New(cfg, bc, state, mempool, exec, emitter, w1.PrivKey())
	poa2 := New(cfg, bc, state, mempool, exec, emitter, w2.PrivKey())
	poa3 := New(cfg, bc, state, mempool, exec, emitter, w3.PrivKey())

	if poa1.IsProposer() {
		t.Error("w1 should not be proposer at height 1 (idx=1)")
	}
	if !poa2.IsProposer() {
		t.Error("w2 should be proposer at height 1 (idx=1)")
	}
	if poa3.IsProposer() {
		t.Error("w3 should not be proposer at height 1 (idx=1)")
	}
}

// --- ProduceBlock tests ---

func TestProduceBlock_NotProposer(t *testing.T) {
	w1, _ := wallet.Generate()
	w2, _ := wallet.Generate()

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	_ = bc.Init()

	// w1 is at index 0, w2 at index 1. Height 0 (genesis) means next = 1, idx = 1 % 2 = 1 -> w2.
	cfg := &config.Config{
		NodeID:      "test",
		MaxBlockTxs: 500,
		Validators:  []string{w1.PubKey(), w2.PubKey()},
		Genesis: config.GenesisConfig{
			ChainID: testChainID,
			Alloc:   map[string]uint64{w1.PubKey(): 1_000_000},
		},
	}

	genesis, _ := config.CreateGenesisBlock(cfg, state, w1.PrivKey())
	_ = bc.AddBlock(genesis)

	emitter := events.NewEmitter()
	mempool := core.NewMempool()
	exec := vm.NewExecutor(state, emitter)

	// w1 is not the proposer for height 1
	poa := New(cfg, bc, state, mempool, exec, emitter, w1.PrivKey())
	_, err := poa.ProduceBlock()
	if err == nil {
		t.Fatal("expected error when not the proposer")
	}
	if err.Error() != "not the proposer for this round" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestProduceBlock_EmptyMempool(t *testing.T) {
	poa, bc, _ := setupPoA(t)

	block, err := poa.ProduceBlock()
	if err != nil {
		t.Fatalf("ProduceBlock with empty mempool should succeed: %v", err)
	}
	if len(block.Transactions) != 0 {
		t.Errorf("expected 0 transactions, got %d", len(block.Transactions))
	}
	if block.Header.Height != 1 {
		t.Errorf("expected height 1, got %d", block.Header.Height)
	}
	if block.Header.StateRoot == "" {
		t.Error("StateRoot should be set")
	}
	if block.Header.TxRoot == "" {
		t.Error("TxRoot should be set")
	}
	if bc.Height() != 1 {
		t.Errorf("blockchain height: got %d want 1", bc.Height())
	}
}

// --- ValidateBlock tests ---

func TestValidateBlock_InvalidSignature(t *testing.T) {
	poa, bc, w := setupPoA(t)

	tip := bc.Tip()
	block := core.NewBlock(testChainID, tip.Header.Height+1, tip.Hash, w.PubKey(), nil)
	// Sign with a different (wrong) key
	other, _ := wallet.Generate()
	block.Sign(other.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block with invalid signature")
	}
}

func TestValidateBlock_WrongProposer(t *testing.T) {
	w1, _ := wallet.Generate()
	w2, _ := wallet.Generate()

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	_ = bc.Init()

	cfg := &config.Config{
		NodeID:      "test",
		MaxBlockTxs: 500,
		Validators:  []string{w1.PubKey(), w2.PubKey()},
		Genesis: config.GenesisConfig{
			ChainID: testChainID,
			Alloc:   map[string]uint64{w1.PubKey(): 1_000_000},
		},
	}

	genesis, _ := config.CreateGenesisBlock(cfg, state, w1.PrivKey())
	_ = bc.AddBlock(genesis)

	emitter := events.NewEmitter()
	mempool := core.NewMempool()
	exec := vm.NewExecutor(state, emitter)
	poa := New(cfg, bc, state, mempool, exec, emitter, w1.PrivKey())

	tip := bc.Tip()
	// Height 1, idx = 1 % 2 = 1 -> expected w2. Build block claiming w1 is proposer.
	block := core.NewBlock(testChainID, tip.Header.Height+1, tip.Hash, w1.PubKey(), nil)
	block.Sign(w1.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block with wrong proposer")
	}
}

func TestValidateBlock_BadTimestamp_Future(t *testing.T) {
	poa, bc, w := setupPoA(t)

	tip := bc.Tip()
	block := core.NewBlock(testChainID, tip.Header.Height+1, tip.Hash, w.PubKey(), nil)
	block.Header.Timestamp = time.Now().Add(30 * time.Second).UnixNano()
	block.Sign(w.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block with future timestamp beyond drift")
	}
}

func TestValidateBlock_BadChainID(t *testing.T) {
	poa, bc, w := setupPoA(t)

	tip := bc.Tip()
	block := core.NewBlock("wrong-chain-id", tip.Header.Height+1, tip.Hash, w.PubKey(), nil)
	block.Sign(w.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block with wrong chain ID")
	}
}

func TestValidateBlock_NoValidators(t *testing.T) {
	w, _ := wallet.Generate()

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	_ = bc.Init()

	cfg := &config.Config{
		NodeID:     "test",
		Validators: nil,
		Genesis:    config.GenesisConfig{ChainID: testChainID},
	}

	emitter := events.NewEmitter()
	mempool := core.NewMempool()
	exec := vm.NewExecutor(state, emitter)
	poa := New(cfg, bc, state, mempool, exec, emitter, w.PrivKey())

	block := core.NewBlock(testChainID, 1, config.GenesisHash, w.PubKey(), nil)
	block.Sign(w.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block when no validators configured")
	}
}
