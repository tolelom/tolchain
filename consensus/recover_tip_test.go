package consensus

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/wallet"
)

func produceWithTransfer(t *testing.T, poa *PoA, w *wallet.Wallet, nonce uint64) *core.Block {
	t.Helper()
	if err := poa.mempool.Add(signedTransfer(t, w, nonce)); err != nil {
		t.Fatal(err)
	}
	block, err := poa.ProduceBlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Transactions) != 1 {
		t.Fatalf("setup: want 1 tx in block, got %d", len(block.Transactions))
	}
	return block
}

func TestRecoverTipState_ReplaysTipWhenStateIsOneBlockBehind(t *testing.T) {
	w, _ := wallet.Generate()
	producer, _ := setupPoAFor(t, w)
	block := produceWithTransfer(t, producer, w, 0)

	crashed, bc := setupPoAFor(t, w)
	if err := bc.AddBlock(block); err != nil {
		t.Fatal(err)
	}

	recovered, err := crashed.RecoverTipState()
	if err != nil || !recovered {
		t.Fatalf("want recovery, got recovered=%v err=%v", recovered, err)
	}
	root, _ := crashed.state.ComputeRoot()
	if root != block.Header.StateRoot {
		t.Fatal("state root after recovery does not match the tip")
	}
	acc, _ := crashed.state.GetAccount(w.PubKey())
	if acc.Nonce != 1 {
		t.Fatalf("tip tx not applied: nonce %d", acc.Nonce)
	}
	again, err := crashed.RecoverTipState()
	if err != nil || again {
		t.Fatalf("recovered state must not replay again, got recovered=%v err=%v", again, err)
	}
}

func TestRecoverTipState_LeavesConsistentStateAlone(t *testing.T) {
	poa, _, w := setupPoA(t)
	produceWithTransfer(t, poa, w, 0)
	recovered, err := poa.RecoverTipState()
	if err != nil || recovered {
		t.Fatalf("consistent node must not replay, got recovered=%v err=%v", recovered, err)
	}
}

func TestRecoverTipState_RefusesStateMoreThanOneBlockBehind(t *testing.T) {
	w, _ := wallet.Generate()
	producer, _ := setupPoAFor(t, w)
	first := produceWithTransfer(t, producer, w, 0)
	second := produceWithTransfer(t, producer, w, 1)

	crashed, bc := setupPoAFor(t, w)
	for _, b := range []*core.Block{first, second} {
		if err := bc.AddBlock(b); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := crashed.RecoverTipState(); err == nil {
		t.Fatal("state two blocks behind must be reported, not silently replayed")
	}
	root, _ := crashed.state.ComputeRoot()
	if root == second.Header.StateRoot {
		t.Fatal("refused recovery must not leave partial writes")
	}
}
