package tests

import (
	"path/filepath"
	"testing"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/consensus"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"
)

// revealAtDeadlineWithPruning은 블록 생성 노드처럼 블록마다 프루닝하면서 높이 1에 커밋하고
// 공개 마감 높이에서 공개한다. 공개된 약속과 목표 블록 해시를 돌려준다.
func revealAtDeadlineWithPruning(t *testing.T, keepBlocks int64) (*core.RandomCommitment, string) {
	t.Helper()
	w, err := wallet.Generate()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		NodeID:      "test",
		MaxBlockTxs: 500,
		Validators:  []string{w.PubKey()},
		Operators:   []string{w.PubKey()},
		Genesis: config.GenesisConfig{
			ChainID: testChainID,
			Alloc:   map[string]uint64{w.PubKey(): 1_000_000},
		},
	}
	cfg.ApplyDefaults()

	db, err := storage.NewLevelDB(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	state := storage.NewStateDB(db)
	bc := core.NewBlockchain(storage.NewLevelBlockStore(db))
	if err := bc.Init(); err != nil {
		t.Fatal(err)
	}
	genesis, err := config.CreateGenesisBlock(cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatal(err)
	}

	emitter := events.NewEmitter()
	mempool := core.NewMempool(cfg.Mempool.MaxSize, cfg.Mempool.MaxTxAgeSec, cfg.Mempool.MaxFutureSec)
	exec := vm.NewExecutor(state, emitter)
	exec.SetOperators(cfg.Operators)
	exec.SetBlockHashes(func(height int64) (string, error) {
		b, err := bc.GetBlockByHeight(height)
		if err != nil {
			return "", err
		}
		return b.Hash, nil
	})
	poa := consensus.New(cfg, bc, state, mempool, exec, emitter, w.PrivKey())
	pruner := storage.NewPruner(db, keepBlocks)

	produce := func(txs ...*core.Transaction) *core.Block {
		t.Helper()
		for _, tx := range txs {
			if err := mempool.Add(tx); err != nil {
				t.Fatal(err)
			}
		}
		b, err := poa.ProduceBlock()
		if err != nil {
			t.Fatal(err)
		}
		if err := pruner.Prune(b.Header.Height); err != nil {
			t.Fatal(err)
		}
		return b
	}

	secret := "prune-secret"
	commitTx, err := w.RandomCommit(testChainID, "drop-prune", crypto.Hash([]byte(secret)), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	produce(commitTx)

	target := int64(1 + core.RandomTargetDelay)
	deadline := target + core.RandomRevealWindow
	var targetHash string
	for h := int64(2); h < deadline; h++ {
		b := produce()
		if h == target {
			targetHash = b.Hash
		}
	}
	revealTx, err := w.RandomReveal(testChainID, "drop-prune", secret, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if b := produce(revealTx); b.Header.Height != deadline {
		t.Fatalf("공개 블록 높이 %d, 기대 %d", b.Header.Height, deadline)
	}
	rc, err := state.GetRandomCommitment("drop-prune")
	if err != nil {
		t.Fatal(err)
	}
	return rc, targetHash
}

// 허용하는 최소 보존 블록 수로 프루닝해도 마감 높이의 공개가 목표 블록 해시를 읽는다.
func TestRandomReveal_PruningAtMinimumKeepsTarget(t *testing.T) {
	rc, targetHash := revealAtDeadlineWithPruning(t, config.MinPruneKeepBlocks)
	if !rc.Revealed {
		t.Fatal("마감 높이의 공개가 실행되지 않았다")
	}
	if want := crypto.Hash([]byte("prune-secret" + targetHash)); rc.Result != want {
		t.Fatalf("result: got %s want %s", rc.Result, want)
	}
}

// 최소값보다 적게 보존하면 목표 블록이 지워져 공개가 실패한다. 최소값 검증의 근거다.
func TestRandomReveal_PruningBelowMinimumLosesTarget(t *testing.T) {
	rc, _ := revealAtDeadlineWithPruning(t, 50)
	if rc.Revealed {
		t.Fatal("목표 블록이 지워졌는데 공개가 실행됐다")
	}
}
