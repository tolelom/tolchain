package consensus

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	_ "github.com/tolelom/tolchain/vm/modules/economy"
	"github.com/tolelom/tolchain/wallet"
)

func signedTransfer(t *testing.T, w *wallet.Wallet, nonce uint64) *core.Transaction {
	t.Helper()
	tx, err := core.NewTransaction(testChainID, core.TxTransfer, w.PubKey(), nonce, 0, core.TransferPayload{To: w.PubKey(), Amount: 1})
	if err != nil {
		t.Fatal(err)
	}
	tx.Sign(w.PrivKey())
	return tx
}

func TestProduceBlock_IncludesSameSenderTxsArrivingOutOfNonceOrder(t *testing.T) {
	poa, _, w := setupPoA(t)
	second, first := signedTransfer(t, w, 1), signedTransfer(t, w, 0)
	for _, tx := range []*core.Transaction{second, first} {
		if err := poa.mempool.Add(tx); err != nil {
			t.Fatal(err)
		}
	}

	block, err := poa.ProduceBlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Transactions) != 2 || block.Transactions[0].ID != first.ID || block.Transactions[1].ID != second.ID {
		t.Fatalf("want both txs in nonce order, got %d txs", len(block.Transactions))
	}
}

func TestProduceBlock_KeepsFutureNonceTxUntilGapFills(t *testing.T) {
	poa, _, w := setupPoA(t)
	second := signedTransfer(t, w, 1)
	if err := poa.mempool.Add(second); err != nil {
		t.Fatal(err)
	}
	block, err := poa.ProduceBlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Transactions) != 0 {
		t.Fatalf("future-nonce tx must wait, got %d txs", len(block.Transactions))
	}
	if _, ok := poa.mempool.Get(second.ID); !ok {
		t.Fatal("future-nonce tx was dropped from the mempool")
	}

	if err := poa.mempool.Add(signedTransfer(t, w, 0)); err != nil {
		t.Fatal(err)
	}
	block, err = poa.ProduceBlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Transactions) != 2 {
		t.Fatalf("want both txs once the gap fills, got %d", len(block.Transactions))
	}
}

func TestMempoolPending_SortsEachSenderByNonceInPlace(t *testing.T) {
	poa, _, w := setupPoA(t)
	other, err := wallet.Generate()
	if err != nil {
		t.Fatal(err)
	}
	a1, b0, a0 := signedTransfer(t, w, 1), signedTransfer(t, other, 0), signedTransfer(t, w, 0)
	for _, tx := range []*core.Transaction{a1, b0, a0} {
		if err := poa.mempool.Add(tx); err != nil {
			t.Fatal(err)
		}
	}
	got := poa.mempool.Pending(10)
	if len(got) != 3 || got[0].ID != a0.ID || got[1].ID != b0.ID || got[2].ID != a1.ID {
		t.Fatal("want [a0, b0, a1]: each sender's txs sorted by nonce in the slots they occupy")
	}
}
