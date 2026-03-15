package tests

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	_ "github.com/tolelom/tolchain/vm/modules/random"
)

func TestCommitReveal(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	operator, _ := wallet.Generate()
	exec.SetOperators([]string{operator.PubKey()})
	_ = state.SetAccount(&core.Account{Address: operator.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", operator.PubKey(), nil)

	secret := "my-secret-value-12345"
	commitHash := crypto.Hash([]byte(secret))

	// Commit
	commitTx, _ := operator.RandomCommit("test-chain", "drop-001", commitHash, 0, 0)
	if err := exec.ExecuteTx(block, commitTx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	rc, err := state.GetRandomCommitment("drop-001")
	if err != nil {
		t.Fatal(err)
	}
	if rc.Revealed {
		t.Error("should not be revealed yet")
	}

	// Reveal (must be at least minRevealDelay=2 blocks after commit at height 1)
	revealBlock := core.NewBlock("test-chain", 4, "prev", operator.PubKey(), nil)
	revealTx, _ := operator.RandomReveal("test-chain", "drop-001", secret, 1, 0)
	if err := exec.ExecuteTx(revealBlock, revealTx); err != nil {
		t.Fatalf("reveal: %v", err)
	}

	rc, _ = state.GetRandomCommitment("drop-001")
	if !rc.Revealed {
		t.Error("should be revealed")
	}
	expectedResult := crypto.Hash([]byte(secret + revealBlock.Header.PrevHash))
	if rc.Result != expectedResult {
		t.Errorf("result: got %s want %s", rc.Result, expectedResult)
	}
}

func TestRevealWrongSecret(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	operator, _ := wallet.Generate()
	exec.SetOperators([]string{operator.PubKey()})
	_ = state.SetAccount(&core.Account{Address: operator.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", operator.PubKey(), nil)

	commitHash := crypto.Hash([]byte("correct-secret"))
	commitTx, _ := operator.RandomCommit("test-chain", "drop-002", commitHash, 0, 0)
	_ = exec.ExecuteTx(block, commitTx)

	// Wrong secret (reveal at height 4 to satisfy minRevealDelay)
	revealBlock := core.NewBlock("test-chain", 4, "prev", operator.PubKey(), nil)
	revealTx, _ := operator.RandomReveal("test-chain", "drop-002", "wrong-secret", 1, 0)
	if err := exec.ExecuteTx(revealBlock, revealTx); err == nil {
		t.Error("should fail: wrong secret")
	}
}

func TestDoubleReveal(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	operator, _ := wallet.Generate()
	exec.SetOperators([]string{operator.PubKey()})
	_ = state.SetAccount(&core.Account{Address: operator.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", operator.PubKey(), nil)

	secret := "my-secret"
	commitHash := crypto.Hash([]byte(secret))
	commitTx, _ := operator.RandomCommit("test-chain", "drop-003", commitHash, 0, 0)
	_ = exec.ExecuteTx(block, commitTx)

	revealBlock := core.NewBlock("test-chain", 4, "prev", operator.PubKey(), nil)
	revealTx, _ := operator.RandomReveal("test-chain", "drop-003", secret, 1, 0)
	_ = exec.ExecuteTx(revealBlock, revealTx)

	// Second reveal should fail
	revealTx2, _ := operator.RandomReveal("test-chain", "drop-003", secret, 2, 0)
	if err := exec.ExecuteTx(revealBlock, revealTx2); err == nil {
		t.Error("should fail: already revealed")
	}
}
