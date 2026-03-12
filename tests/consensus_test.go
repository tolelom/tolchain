package tests

import (
	"testing"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/consensus"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"
)

func newTestPoA(t *testing.T) (*consensus.PoA, *core.Blockchain, *wallet.Wallet) {
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
	cfg.ApplyDefaults()

	genesis, err := config.CreateGenesisBlock(cfg, state, w.PrivKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatal(err)
	}

	emitter := events.NewEmitter()
	mempool := core.NewMempool(cfg.Mempool.MaxSize, cfg.Mempool.MaxTxAgeSec, cfg.Mempool.MaxFutureSec)
	exec := vm.NewExecutor(state, emitter)
	poa := consensus.New(cfg, bc, state, mempool, exec, emitter, w.PrivKey())

	return poa, bc, w
}

func TestIsProposer(t *testing.T) {
	poa, _, _ := newTestPoA(t)

	// With a single validator, this node should always be the proposer.
	if !poa.IsProposer() {
		t.Error("single validator should always be proposer")
	}
}

func TestIsProposer_MultipleValidators(t *testing.T) {
	w1, _ := wallet.Generate()
	w2, _ := wallet.Generate()
	w3, _ := wallet.Generate()

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	_ = bc.Init()

	cfg := &config.Config{
		NodeID:      "test",
		MaxBlockTxs: 500,
		Validators:  []string{w1.PubKey(), w2.PubKey(), w3.PubKey()},
		Genesis: config.GenesisConfig{
			ChainID: testChainID,
			Alloc:   map[string]uint64{w1.PubKey(): 1_000_000},
		},
	}
	cfg.ApplyDefaults()

	genesis, _ := config.CreateGenesisBlock(cfg, state, w1.PrivKey())
	_ = bc.AddBlock(genesis)

	emitter := events.NewEmitter()
	mempool := core.NewMempool(cfg.Mempool.MaxSize, cfg.Mempool.MaxTxAgeSec, cfg.Mempool.MaxFutureSec)
	exec := vm.NewExecutor(state, emitter)

	// Genesis is height 0. Next height = 1. idx = 1 % 3 = 1 → w2.
	poa1 := consensus.New(cfg, bc, state, mempool, exec, emitter, w1.PrivKey())
	poa2 := consensus.New(cfg, bc, state, mempool, exec, emitter, w2.PrivKey())

	if poa1.IsProposer() {
		t.Error("w1 should not be proposer at height 1")
	}
	if !poa2.IsProposer() {
		t.Error("w2 should be proposer at height 1")
	}
}

func TestValidateBlock_OK(t *testing.T) {
	poa, bc, w := newTestPoA(t)

	tip := bc.Tip()
	block := core.NewBlock(testChainID, tip.Header.Height+1, tip.Hash, w.PubKey(), nil)
	block.Header.StateRoot = ""
	block.Sign(w.PrivKey())

	// Validating a correctly built block should succeed.
	if err := poa.ValidateBlock(block); err != nil {
		t.Fatalf("ValidateBlock should pass: %v", err)
	}
}

func TestValidateBlock_BadChainID(t *testing.T) {
	poa, bc, w := newTestPoA(t)

	tip := bc.Tip()
	block := core.NewBlock("wrong-chain", tip.Header.Height+1, tip.Hash, w.PubKey(), nil)
	block.Sign(w.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block with wrong chain ID")
	}
}

func TestValidateBlock_BadSignature(t *testing.T) {
	poa, bc, w := newTestPoA(t)

	tip := bc.Tip()
	block := core.NewBlock(testChainID, tip.Header.Height+1, tip.Hash, w.PubKey(), nil)
	// Sign with a different key
	other, _ := wallet.Generate()
	block.Sign(other.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block with bad signature")
	}
}

func TestValidateBlock_FutureTimestamp(t *testing.T) {
	poa, bc, w := newTestPoA(t)

	tip := bc.Tip()
	block := core.NewBlock(testChainID, tip.Header.Height+1, tip.Hash, w.PubKey(), nil)
	block.Header.Timestamp = time.Now().Add(30 * time.Second).UnixNano()
	block.Sign(w.PrivKey())

	err := poa.ValidateBlock(block)
	if err == nil {
		t.Error("should reject block with future timestamp > 15s")
	}
}

func TestProduceBlock(t *testing.T) {
	poa, bc, _ := newTestPoA(t)

	block, err := poa.ProduceBlock()
	if err != nil {
		t.Fatal(err)
	}
	if block.Header.Height != 1 {
		t.Errorf("height: got %d want 1", block.Header.Height)
	}
	if block.Header.TxRoot == "" {
		t.Error("TxRoot should be set")
	}
	if block.Header.StateRoot == "" {
		t.Error("StateRoot should be set")
	}
	if bc.Height() != 1 {
		t.Errorf("blockchain height: got %d want 1", bc.Height())
	}
}
