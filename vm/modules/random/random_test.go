package random

import (
	"encoding/json"
	"fmt"
	"strings"
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
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter, EffectiveSender: fromPubHex,
		BlockHash: blockHashes(map[int64]string{2: "target-hash"}), Operators: map[string]bool{fromPubHex: true}}
}

// blockHashes는 높이별 블록 해시 표로 Context.BlockHash를 만든다.
func blockHashes(m map[int64]string) func(int64) (string, error) {
	return func(h int64) (string, error) {
		if hash, ok := m[h]; ok {
			return hash, nil
		}
		return "", fmt.Errorf("block %d: %w", h, core.ErrNotFound)
	}
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
	ctx.Block.Header.Height = 5 // 목표 블록 0+2 다음부터 공개 가능

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

	// 결과는 공개 블록이 아니라 목표 블록(커밋 높이 0 + 2)의 해시에 묶인다.
	expectedResult := crypto.Hash([]byte(secret + "target-hash"))
	if rc.Result != expectedResult {
		t.Errorf("result = %s, want %s", rc.Result, expectedResult)
	}
}

// revealAt은 높이 0에 커밋된 c1을 height 블록(직전 해시 prevHash)에서 공개한다.
func revealAt(t *testing.T, height int64, prevHash string, hashes map[int64]string) (*core.RandomCommitment, error) {
	t.Helper()
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	secret := "my-secret"
	_ = state.SetRandomCommitment(&core.RandomCommitment{ID: "c1", Committer: pub.Hex(), CommitHash: crypto.Hash([]byte(secret))})
	ctx := newCtx(t, state, pub.Hex())
	ctx.Tx.Type = core.TxRandomReveal
	ctx.Block.Header.Height = height
	ctx.Block.Header.PrevHash = prevHash
	if hashes != nil {
		ctx.BlockHash = blockHashes(hashes)
	}
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: secret})
	if err := handleRandomReveal(ctx, payload); err != nil {
		return nil, err
	}
	rc, _ := state.GetRandomCommitment("c1")
	return rc, nil
}

// 공개 시점을 골라 결과를 바꿀 수 없다: 어느 블록에서 공개해도 결과가 같다.
func TestRandomReveal_ResultIndependentOfRevealBlock(t *testing.T) {
	hashes := map[int64]string{2: "target-hash"}
	early, err := revealAt(t, 3, "hash-2", hashes)
	if err != nil {
		t.Fatal(err)
	}
	late, err := revealAt(t, 50, "hash-49", hashes)
	if err != nil {
		t.Fatal(err)
	}
	if early.Result != late.Result {
		t.Fatalf("공개 블록에 따라 결과가 다르다: %s, %s", early.Result, late.Result)
	}
}

func wantErr(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Fatalf("오류에 %q가 있어야 한다: %v", sub, err)
	}
}

func TestRandomReveal_Window(t *testing.T) {
	hashes := map[int64]string{2: "target-hash"}
	// 목표 블록 2, 공개는 3부터 2+100까지
	_, err := revealAt(t, 2, "p", hashes)
	wantErr(t, err, "reveal allowed from height 3")
	if _, err := revealAt(t, 3, "p", hashes); err != nil {
		t.Fatalf("목표 블록 다음 높이의 공개는 받아야 한다: %v", err)
	}
	if _, err := revealAt(t, 102, "p", hashes); err != nil {
		t.Fatalf("마감 높이의 공개는 받아야 한다: %v", err)
	}
	_, err = revealAt(t, 103, "p", hashes)
	wantErr(t, err, "reveal expired")
}

func TestRandomReveal_TargetHashUnavailable(t *testing.T) {
	_, err := revealAt(t, 5, "p", map[int64]string{})
	wantErr(t, err, "target block 2")
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_ = state.SetRandomCommitment(&core.RandomCommitment{ID: "c1", Committer: pub.Hex(), CommitHash: crypto.Hash([]byte("s"))})
	ctx := newCtx(t, state, pub.Hex())
	ctx.Block.Header.Height = 5
	ctx.BlockHash = nil
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: "s"})
	wantErr(t, handleRandomReveal(ctx, payload), "not configured")
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

	// 공개 기간이 지난 높이에서도 이미 공개됐다는 오류가 먼저 나온다.
	ctx := newCtx(t, state, pubHex)
	ctx.Block.Header.Height = 500
	payload, _ := json.Marshal(core.RandomRevealPayload{CommitID: "c1", Secret: "anything"})

	wantErr(t, handleRandomReveal(ctx, payload), "already revealed")
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
	ctx.Block.Header.Height = 5 // 목표 블록 0+2 다음부터 공개 가능

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
