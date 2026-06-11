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

// TestProduceBlock_TimestampDeterministicReplay is a regression test for the
// double core.NewBlock timestamp bug: handlers store the executing block's
// timestamp into state (e.g. DelegationGrant.CreatedAt, Asset.MintedAt), so
// re-executing the broadcast block from scratch on a fresh node must reproduce
// the exact StateRoot recorded in the header. If the broadcast header carries
// a different timestamp than the one used during execution, syncing nodes hit
// a StateRoot mismatch and sync halts forever.
func TestProduceBlock_TimestampDeterministicReplay(t *testing.T) {
	w, err := wallet.Generate()
	if err != nil {
		t.Fatal(err)
	}
	grantee, err := wallet.Generate()
	if err != nil {
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

	// --- producer node ---
	stateA := storage.NewStateDB(testutil.NewMemDB())
	bcA := core.NewBlockchain(testutil.NewMemBlockStore())
	if err := bcA.Init(); err != nil {
		t.Fatal(err)
	}
	genesisA, err := config.CreateGenesisBlock(cfg, stateA, w.PrivKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := bcA.AddBlock(genesisA); err != nil {
		t.Fatal(err)
	}

	mempool := core.NewMempool(cfg.Mempool.MaxSize, cfg.Mempool.MaxTxAgeSec, cfg.Mempool.MaxFutureSec)
	execA := vm.NewExecutor(stateA, events.NewEmitter())
	poa := consensus.New(cfg, bcA, stateA, mempool, execA, events.NewEmitter(), w.PrivKey())

	// --- fresh syncing node, fed the broadcast blocks ---
	stateB := storage.NewStateDB(testutil.NewMemDB())
	if _, err := config.CreateGenesisBlock(cfg, stateB, w.PrivKey()); err != nil {
		t.Fatal(err)
	}
	execB := vm.NewExecutor(stateB, events.NewEmitter())

	// Produce many blocks: the bug only manifests when the two time.Now()
	// calls inside ProduceBlock land on different clock ticks, so a single
	// block can pass by luck on coarse-resolution clocks.
	const rounds = 300
	for i := 0; i < rounds; i++ {
		// grant_delegation stores ctx.Block.Header.Timestamp (CreatedAt) in state.
		grantTx, err := w.GrantDelegation(testChainID, grantee.PubKey(), []string{"transfer"}, 0, 0, 0, uint64(i), 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := mempool.Add(grantTx); err != nil {
			t.Fatal(err)
		}

		block, err := poa.ProduceBlock()
		if err != nil {
			t.Fatal(err)
		}
		if len(block.Transactions) != 1 {
			t.Fatalf("round %d: expected 1 tx in block, got %d", i, len(block.Transactions))
		}

		// Re-execute the broadcast block on the fresh node, like sync does.
		if err := execB.ExecuteBlock(block); err != nil {
			t.Fatalf("round %d: re-execute broadcast block: %v", i, err)
		}
		replayRoot, err := stateB.ComputeRoot()
		if err != nil {
			t.Fatal(err)
		}
		if replayRoot != block.Header.StateRoot {
			t.Fatalf("round %d: replayed StateRoot %s != header StateRoot %s (block timestamp not deterministic)", i, replayRoot, block.Header.StateRoot)
		}
		if err := stateB.Commit(); err != nil {
			t.Fatal(err)
		}
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
