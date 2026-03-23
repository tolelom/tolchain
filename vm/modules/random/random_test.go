package random

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/vm"
)

func newCtx(t *testing.T, state core.State, fromPubHex string) *vm.Context {
	t.Helper()
	emitter := events.NewEmitter()
	block := &core.Block{Header: core.BlockHeader{
		Height:    1,
		Timestamp: time.Now().UnixNano(),
		PrevHash:  "abc123",
	}}
	tx := &core.Transaction{ID: "tx1", From: fromPubHex, Type: core.TxRandomCommit}
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter, EffectiveSender: fromPubHex}
}

// ---------- Commit Tests ----------

func TestRandomCommit_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	secret := "my-secret"
	commitHash := crypto.Hash([]byte(secret))

	ctx := newCtx(t, state, pubHex)
	// No operators set = no restriction.
	payload, _ := json.Marshal(core.RandomCommitPayload{
		CommitID:   "c1",
		CommitHash: commitHash,
	})

	if err := handleRandomCommit(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	rc, err := state.GetRandomCommitment("c1")
	if err != nil {
		t.Fatalf("commitment not found: %v", err)
	}
	if rc.Committer != pubHex {
		t.Errorf("committer = %s, want %s", rc.Committer, pubHex)
	}
	if rc.CommitHash != commitHash {
		t.Errorf("commit_hash = %s, want %s", rc.CommitHash, commitHash)
	}
}

func TestRandomCommit_EmptyFields(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	// Empty commit_id
	payload, _ := json.Marshal(core.RandomCommitPayload{CommitID: "", CommitHash: "aabb"})
	if err := handleRandomCommit(ctx, payload); err == nil {
		t.Fatal("expected error for empty commit_id")
	}

	// Empty commit_hash
	payload, _ = json.Marshal(core.RandomCommitPayload{CommitID: "c1", CommitHash: ""})
	if err := handleRandomCommit(ctx, payload); err == nil {
		t.Fatal("expected error for empty commit_hash")
	}
}

func TestRandomCommit_NonHexCommitHash(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.RandomCommitPayload{CommitID: "c1", CommitHash: "not-hex!"})

	if err := handleRandomCommit(ctx, payload); err == nil {
		t.Fatal("expected error for non-hex commit_hash")
	}
}

func TestRandomCommit_Duplicate(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetRandomCommitment(&core.RandomCommitment{ID: "c1", Committer: pubHex, CommitHash: "aabb"})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.RandomCommitPayload{CommitID: "c1", CommitHash: "aabb"})

	if err := handleRandomCommit(ctx, payload); err == nil {
		t.Fatal("expected error for duplicate commitment")
	}
}

func TestRandomCommit_NonOperator(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, opPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	ctx := newCtx(t, state, pubHex)
	ctx.Operators = map[string]bool{opPub.Hex(): true}

	payload, _ := json.Marshal(core.RandomCommitPayload{CommitID: "c1", CommitHash: "aabb"})

	if err := handleRandomCommit(ctx, payload); err == nil {
		t.Fatal("expected error for non-operator")
	}
}

// ---------- Reveal Tests ----------

func TestRandomReveal_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	secret := "my-secret"
	commitHash := crypto.Hash([]byte(secret))
	prevHash := "abc123"

	_ = state.SetRandomCommitment(&core.RandomCommitment{
		ID:         "c1",
		Committer:  pubHex,
		CommitHash: commitHash,
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxRandomReveal
	ctx.Block.Header.PrevHash = prevHash
	ctx.Block.Header.Height = 5 // must be > BlockHeight + minRevealDelay (0 + 2)

	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: secret})

	if err := handleRandomReveal(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	rc, _ := state.GetRandomCommitment("c1")
	if !rc.Revealed {
		t.Error("commitment should be revealed")
	}
	if rc.Secret != secret {
		t.Errorf("secret = %s, want %s", rc.Secret, secret)
	}

	expectedResult := crypto.Hash([]byte(secret + prevHash))
	if rc.Result != expectedResult {
		t.Errorf("result = %s, want %s", rc.Result, expectedResult)
	}
}

func TestRandomReveal_WrongSecret(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	secret := "my-secret"
	commitHash := crypto.Hash([]byte(secret))

	_ = state.SetRandomCommitment(&core.RandomCommitment{
		ID:         "c1",
		Committer:  pubHex,
		CommitHash: commitHash,
	})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: "wrong-secret"})

	if err := handleRandomReveal(ctx, payload); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestRandomReveal_AlreadyRevealed(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetRandomCommitment(&core.RandomCommitment{
		ID:         "c1",
		Committer:  pubHex,
		CommitHash: "aabb",
		Revealed:   true,
	})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: "anything"})

	if err := handleRandomReveal(ctx, payload); err == nil {
		t.Fatal("expected error for already revealed commitment")
	}
}

func TestRandomReveal_NonCommitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetRandomCommitment(&core.RandomCommitment{
		ID:         "c1",
		Committer:  pub2.Hex(),
		CommitHash: "aabb",
	})

	ctx := newCtx(t, state, pub.Hex()) // not the committer
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: "anything"})

	if err := handleRandomReveal(ctx, payload); err == nil {
		t.Fatal("expected error for non-committer reveal")
	}
}

func TestRandomReveal_NonexistentCommitment(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "nonexistent", Secret: "anything"})

	if err := handleRandomReveal(ctx, payload); err == nil {
		t.Fatal("expected error for nonexistent commitment")
	}
}

func TestRandomReveal_NonOperator(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, opPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetRandomCommitment(&core.RandomCommitment{
		ID:         "c1",
		Committer:  pubHex,
		CommitHash: crypto.Hash([]byte("secret")),
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Operators = map[string]bool{opPub.Hex(): true} // pubHex is NOT an operator

	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: "secret"})

	if err := handleRandomReveal(ctx, payload); err == nil {
		t.Fatal("expected error for non-operator reveal")
	}
}

func TestRandomCommit_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	if err := handleRandomCommit(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestRandomReveal_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	if err := handleRandomReveal(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestRandomCommit_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	secret := "nil-emitter-secret"
	commitHash := crypto.Hash([]byte(secret))

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	payload, _ := json.Marshal(core.RandomCommitPayload{
		CommitID:   "c_ne",
		CommitHash: commitHash,
	})

	if err := handleRandomCommit(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestRandomReveal_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	secret := "nil-emitter-reveal"
	commitHash := crypto.Hash([]byte(secret))

	_ = state.SetRandomCommitment(&core.RandomCommitment{
		ID:         "c_ne_r",
		Committer:  pubHex,
		CommitHash: commitHash,
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxRandomReveal
	ctx.Block.Header.Height = 5 // must be > BlockHeight + minRevealDelay (0 + 2)

	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c_ne_r", Secret: secret})

	if err := handleRandomReveal(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestRandomReveal_EmptyFields(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	// Empty commit_id
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "", Secret: "anything"})
	if err := handleRandomReveal(ctx, payload); err == nil {
		t.Fatal("expected error for empty commit_id")
	}

	// Empty secret
	payload, _ = json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: ""})
	if err := handleRandomReveal(ctx, payload); err == nil {
		t.Fatal("expected error for empty secret")
	}
}
