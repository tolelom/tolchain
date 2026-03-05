package tests

import (
	"fmt"
	"testing"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/consensus"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	_ "github.com/tolelom/tolchain/vm/modules/asset"
	_ "github.com/tolelom/tolchain/vm/modules/economy"
	_ "github.com/tolelom/tolchain/vm/modules/inventory"
	_ "github.com/tolelom/tolchain/vm/modules/market"
	_ "github.com/tolelom/tolchain/vm/modules/random"
	_ "github.com/tolelom/tolchain/vm/modules/reward"
	_ "github.com/tolelom/tolchain/vm/modules/session"
)

// ============================================================
// Crypto benchmarks
// ============================================================

func BenchmarkHash(b *testing.B) {
	data := []byte("benchmark data for hashing in tolchain")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		crypto.Hash(data)
	}
}

func BenchmarkSign(b *testing.B) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		b.Fatal(err)
	}
	data := []byte("benchmark data for signing")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		crypto.Sign(priv, data)
	}
}

func BenchmarkVerify(b *testing.B) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		b.Fatal(err)
	}
	data := []byte("benchmark data for verifying")
	sig := crypto.Sign(priv, data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := crypto.Verify(pub, data, sig); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================
// Mempool benchmarks
// ============================================================

func BenchmarkMempoolAdd(b *testing.B) {
	w, _ := wallet.Generate()

	// Pre-generate transactions.
	txs := make([]*core.Transaction, b.N)
	for i := 0; i < b.N; i++ {
		tx, _ := w.NewTx("bench-chain", core.TxTransfer, uint64(i), 0, core.TransferPayload{To: "aa", Amount: 1})
		txs[i] = tx
	}

	b.ResetTimer()
	mp := core.NewMempool()
	for i := 0; i < b.N; i++ {
		// Reset mempool every 9000 txs to avoid hitting the 10,000 cap.
		if i > 0 && i%9000 == 0 {
			mp = core.NewMempool()
		}
		if err := mp.Add(txs[i]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMempoolPending(b *testing.B) {
	mp := core.NewMempool()
	w, _ := wallet.Generate()

	for i := 0; i < 1000; i++ {
		tx, _ := w.NewTx("bench-chain", core.TxTransfer, uint64(i), 0, core.TransferPayload{To: "aa", Amount: 1})
		mp.Add(tx)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mp.Pending(100)
	}
}

// ============================================================
// StateDB benchmarks
// ============================================================

func BenchmarkStateSetAccount(b *testing.B) {
	state := storage.NewStateDB(testutil.NewMemDB())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		addr := fmt.Sprintf("%064x", i)
		state.SetAccount(&core.Account{Address: addr, Balance: uint64(i)})
	}
}

func BenchmarkStateGetAccount(b *testing.B) {
	state := storage.NewStateDB(testutil.NewMemDB())
	addr := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	state.SetAccount(&core.Account{Address: addr, Balance: 1000})
	state.Commit()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.GetAccount(addr)
	}
}

func BenchmarkStateComputeRoot(b *testing.B) {
	state := storage.NewStateDB(testutil.NewMemDB())
	// Seed with 100 accounts to make ComputeRoot meaningful.
	for i := 0; i < 100; i++ {
		addr := fmt.Sprintf("%064x", i)
		state.SetAccount(&core.Account{Address: addr, Balance: uint64(i * 100)})
	}
	state.Commit()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state.ComputeRoot()
	}
}

func BenchmarkStateSnapshotRevert(b *testing.B) {
	state := storage.NewStateDB(testutil.NewMemDB())
	addr := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	state.SetAccount(&core.Account{Address: addr, Balance: 1000})
	state.Commit()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		snapID, _ := state.Snapshot()
		state.SetAccount(&core.Account{Address: addr, Balance: uint64(i)})
		state.RevertToSnapshot(snapID)
	}
}

// ============================================================
// Transaction execution benchmarks
// ============================================================

func BenchmarkExecuteTxTransfer(b *testing.B) {
	state := storage.NewStateDB(testutil.NewMemDB())
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	sender, _ := wallet.Generate()
	receiver, _ := wallet.Generate()
	state.SetAccount(&core.Account{Address: sender.PubKey(), Balance: 1_000_000_000})
	state.Commit()

	block := core.NewBlock("bench-chain", 1, "0000", sender.PubKey(), nil)

	// Pre-generate transactions with sequential nonces.
	txs := make([]*core.Transaction, b.N)
	for i := 0; i < b.N; i++ {
		tx, _ := sender.Transfer("bench-chain", receiver.PubKey(), 1, uint64(i), 0)
		txs[i] = tx
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := exec.ExecuteTx(block, txs[i]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExecuteTxMintAsset(b *testing.B) {
	state := storage.NewStateDB(testutil.NewMemDB())
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	creator, _ := wallet.Generate()
	state.SetAccount(&core.Account{Address: creator.PubKey(), Balance: 1_000_000_000})

	block := core.NewBlock("bench-chain", 1, "0000", creator.PubKey(), nil)

	// Register template first (nonce=0).
	regTx, _ := creator.NewTx("bench-chain", core.TxRegisterTemplate, 0, 0, core.RegisterTemplatePayload{
		ID:        "bench-template",
		Name:      "BenchItem",
		Tradeable: true,
		Schema:    map[string]any{"power": "int"},
	})
	if err := exec.ExecuteTx(block, regTx); err != nil {
		b.Fatal(err)
	}

	// Pre-generate mint transactions.
	txs := make([]*core.Transaction, b.N)
	for i := 0; i < b.N; i++ {
		tx, _ := creator.NewTx("bench-chain", core.TxMintAsset, uint64(i+1), 0, core.MintAssetPayload{
			TemplateID: "bench-template",
			Owner:      creator.PubKey(),
			Properties: map[string]any{"power": i},
		})
		txs[i] = tx
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := exec.ExecuteTx(block, txs[i]); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================
// Block production benchmark
// ============================================================

func BenchmarkProduceBlock(b *testing.B) {
	w, _ := wallet.Generate()
	receiver, _ := wallet.Generate()

	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	bc.Init()

	cfg := &config.Config{
		NodeID:      "bench-node",
		DataDir:     "./data",
		MaxBlockTxs: 500,
		Validators:  []string{w.PubKey()},
		Genesis: config.GenesisConfig{
			ChainID: "bench-chain",
			Alloc:   map[string]uint64{w.PubKey(): 1_000_000_000},
		},
	}

	genesis, _ := config.CreateGenesisBlock(cfg, state, w.PrivKey())
	bc.AddBlock(genesis)

	emitter := events.NewEmitter()
	mempool := core.NewMempool()
	exec := vm.NewExecutor(state, emitter)
	poa := consensus.New(cfg, bc, state, mempool, exec, emitter, w.PrivKey())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Add 10 transactions to the mempool per block.
		nonce := uint64(i * 10)
		for j := 0; j < 10; j++ {
			tx, _ := w.Transfer("bench-chain", receiver.PubKey(), 1, nonce+uint64(j), 0)
			mempool.Add(tx)
		}
		b.StartTimer()

		if _, err := poa.ProduceBlock(); err != nil {
			b.Fatal(err)
		}
	}
}

// ============================================================
// Transaction signing benchmark (wallet)
// ============================================================

func BenchmarkWalletNewTx(b *testing.B) {
	w, _ := wallet.Generate()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Transfer("bench-chain", "deadbeef", 1, uint64(i), 0)
	}
}
