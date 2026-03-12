package session

import (
	"encoding/json"
	"math"
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
	tx := &core.Transaction{ID: "tx1", From: fromPubHex, Type: core.TxSessionOpen}
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter}
}

// ---------- Session Open Tests ----------

func TestSessionOpen_Success(t *testing.T) {
	state := testutil.NewStateDB()
	priv1, pub1, _ := crypto.GenerateKeyPair()
	priv2, pub2, _ := crypto.GenerateKeyPair()
	_ = priv1
	pub1Hex := pub1.Hex()
	pub2Hex := pub2.Hex()

	// Fund both players.
	_ = state.SetAccount(&core.Account{Address: pub1Hex, Balance: 1000})
	_ = state.SetAccount(&core.Account{Address: pub2Hex, Balance: 1000})

	// Player 2 signs consent.
	consentMsg := []byte("session:s1")
	sig := crypto.Sign(priv2, consentMsg)

	ctx := newCtx(t, state, pub1Hex)
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID:  "s1",
		GameID:     "game1",
		Players:    []string{pub1Hex, pub2Hex},
		Stakes:     100,
		Signatures: map[string]string{pub2Hex: sig},
	})

	if err := handleSessionOpen(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	sess, err := state.GetSession("s1")
	if err != nil {
		t.Fatalf("session not found: %v", err)
	}
	if sess.Status != "open" {
		t.Errorf("status = %s, want open", sess.Status)
	}

	// Stakes deducted.
	acc1, _ := state.GetAccount(pub1Hex)
	acc2, _ := state.GetAccount(pub2Hex)
	if acc1.Balance != 900 {
		t.Errorf("player1 balance = %d, want 900", acc1.Balance)
	}
	if acc2.Balance != 900 {
		t.Errorf("player2 balance = %d, want 900", acc2.Balance)
	}
}

func TestSessionOpen_MissingSessionID(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID: "",
		Players:   []string{pub.Hex()},
	})

	if err := handleSessionOpen(ctx, payload); err == nil {
		t.Fatal("expected error for missing session_id")
	}
}

func TestSessionOpen_NoPlayers(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID: "s1",
		Players:   []string{},
	})

	if err := handleSessionOpen(ctx, payload); err == nil {
		t.Fatal("expected error for no players")
	}
}

func TestSessionOpen_InsufficientStakes(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAccount(&core.Account{Address: pubHex, Balance: 50})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID: "s1",
		Players:   []string{pubHex},
		Stakes:    100,
	})

	if err := handleSessionOpen(ctx, payload); err == nil {
		t.Fatal("expected error for insufficient balance for stakes")
	}
}

func TestSessionOpen_MissingConsentSignature(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub1.Hex())
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID:  "s1",
		Players:    []string{pub1.Hex(), pub2.Hex()},
		Signatures: map[string]string{}, // missing p2's signature
	})

	if err := handleSessionOpen(ctx, payload); err == nil {
		t.Fatal("expected error for missing consent signature")
	}
}

func TestSessionOpen_InvalidConsentSignature(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub1.Hex())
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID:  "s1",
		Players:    []string{pub1.Hex(), pub2.Hex()},
		Signatures: map[string]string{pub2.Hex(): "deadbeef"},
	})

	if err := handleSessionOpen(ctx, payload); err == nil {
		t.Fatal("expected error for invalid consent signature")
	}
}

func TestSessionOpen_Duplicate(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{ID: "s1", Status: "open", Players: []string{pubHex}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID: "s1",
		Players:   []string{pubHex},
	})

	if err := handleSessionOpen(ctx, payload); err == nil {
		t.Fatal("expected error for duplicate session")
	}
}

func TestSessionOpen_OperatorRestriction(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, opPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	ctx := newCtx(t, state, pubHex)
	ctx.Operators = map[string]bool{opPub.Hex(): true} // pubHex is NOT an operator

	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID: "s1",
		Players:   []string{pubHex},
	})

	if err := handleSessionOpen(ctx, payload); err == nil {
		t.Fatal("expected error for non-operator")
	}
}

// ---------- Session Result Tests ----------

func TestSessionResult_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	pub1Hex := pub1.Hex()
	pub2Hex := pub2.Hex()

	// Create an open session with stakes.
	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pub1Hex,
		Players: []string{pub1Hex, pub2Hex},
		Stakes:  100,
		Status:  "open",
	})

	ctx := newCtx(t, state, pub1Hex)
	ctx.Tx.Type = core.TxSessionResult
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s1",
		Outcome:   map[string]uint64{pub1Hex: 150, pub2Hex: 50},
	})

	if err := handleSessionResult(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	sess, _ := state.GetSession("s1")
	if sess.Status != "closed" {
		t.Errorf("status = %s, want closed", sess.Status)
	}

	acc1, _ := state.GetAccount(pub1Hex)
	acc2, _ := state.GetAccount(pub2Hex)
	if acc1.Balance != 150 {
		t.Errorf("player1 balance = %d, want 150", acc1.Balance)
	}
	if acc2.Balance != 50 {
		t.Errorf("player2 balance = %d, want 50", acc2.Balance)
	}
}

func TestSessionResult_NonCreator(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pub1.Hex(),
		Players: []string{pub1.Hex(), pub2.Hex()},
		Stakes:  100,
		Status:  "open",
	})

	ctx := newCtx(t, state, pub2.Hex()) // pub2 is not the creator
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s1",
		Outcome:   map[string]uint64{pub1.Hex(): 100, pub2.Hex(): 100},
	})

	if err := handleSessionResult(ctx, payload); err == nil {
		t.Fatal("expected error for non-creator submitting result")
	}
}

func TestSessionResult_ClosedSession(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pubHex,
		Players: []string{pubHex},
		Status:  "closed",
	})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s1",
		Outcome:   map[string]uint64{},
	})

	if err := handleSessionResult(ctx, payload); err == nil {
		t.Fatal("expected error for closed session")
	}
}

func TestSessionResult_NonPlayerRecipient(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	pub1Hex := pub1.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pub1Hex,
		Players: []string{pub1Hex},
		Stakes:  100,
		Status:  "open",
	})

	ctx := newCtx(t, state, pub1Hex)
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s1",
		Outcome:   map[string]uint64{pub2.Hex(): 100}, // pub2 is not a player
	})

	if err := handleSessionResult(ctx, payload); err == nil {
		t.Fatal("expected error for non-player recipient")
	}
}

func TestSessionResult_RewardsNotEqualStakes(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  100,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	// Total stakes = 100, reward = 50 (not equal).
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s1",
		Outcome:   map[string]uint64{pubHex: 50},
	})

	if err := handleSessionResult(ctx, payload); err == nil {
		t.Fatal("expected error for rewards != total stakes")
	}
}

func TestSessionResult_RewardsExceedStakes(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  100,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	// Total stakes = 100, reward = 200 (exceeds).
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s1",
		Outcome:   map[string]uint64{pubHex: 200},
	})

	if err := handleSessionResult(ctx, payload); err == nil {
		t.Fatal("expected error for rewards exceeding total stakes")
	}
}

// ---------- Session Cancel Tests ----------

func TestSessionCancel_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	pub1Hex := pub1.Hex()
	pub2Hex := pub2.Hex()

	// Set initial balances (after stakes were deducted during open).
	_ = state.SetAccount(&core.Account{Address: pub1Hex, Balance: 900})
	_ = state.SetAccount(&core.Account{Address: pub2Hex, Balance: 900})

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pub1Hex,
		Players: []string{pub1Hex, pub2Hex},
		Stakes:  100,
		Status:  "open",
	})

	ctx := newCtx(t, state, pub1Hex)
	ctx.Tx.Type = core.TxSessionCancel
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "s1"})

	if err := handleSessionCancel(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	sess, _ := state.GetSession("s1")
	if sess.Status != "cancelled" {
		t.Errorf("status = %s, want cancelled", sess.Status)
	}

	acc1, _ := state.GetAccount(pub1Hex)
	acc2, _ := state.GetAccount(pub2Hex)
	if acc1.Balance != 1000 {
		t.Errorf("player1 balance = %d, want 1000", acc1.Balance)
	}
	if acc2.Balance != 1000 {
		t.Errorf("player2 balance = %d, want 1000", acc2.Balance)
	}
}

func TestSessionCancel_NonCreator(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pub1.Hex(),
		Players: []string{pub1.Hex()},
		Status:  "open",
	})

	ctx := newCtx(t, state, pub2.Hex()) // not the creator
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "s1"})

	if err := handleSessionCancel(ctx, payload); err == nil {
		t.Fatal("expected error for non-creator cancel")
	}
}

func TestSessionCancel_AlreadyClosed(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pubHex,
		Players: []string{pubHex},
		Status:  "closed",
	})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "s1"})

	if err := handleSessionCancel(ctx, payload); err == nil {
		t.Fatal("expected error for already closed session")
	}
}

func TestSessionCancel_OperatorRestriction(t *testing.T) {
	state := testutil.NewStateDB()
	_, creatorPub, _ := crypto.GenerateKeyPair()
	_, operatorPub, _ := crypto.GenerateKeyPair()
	creatorHex := creatorPub.Hex()

	_ = state.SetSession(&core.Session{ID: "s1", Creator: creatorHex, Status: "open", Players: []string{creatorHex}})

	ctx := newCtx(t, state, creatorHex)
	ctx.Operators = map[string]bool{operatorPub.Hex(): true} // creator is NOT an operator
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "s1"})

	if err := handleSessionCancel(ctx, payload); err == nil {
		t.Fatal("expected error for non-operator cancel")
	}
}

func TestSessionOpen_ZeroStakes(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	p1Hex := pub1.Hex()

	_ = state.SetAccount(&core.Account{Address: p1Hex, Balance: 100})

	ctx := newCtx(t, state, p1Hex)
	// Sender is the only player, so no consent signatures needed.
	// Also, the sender must be an operator if operators are set,
	// so we leave ctx.Operators nil (no restriction).
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID: "s_zero",
		GameID:    "game1",
		Players:   []string{p1Hex},
		Stakes:    0,
	})

	if err := handleSessionOpen(ctx, payload); err != nil {
		t.Fatalf("expected success for zero-stakes session: %v", err)
	}

	// Verify balance unchanged
	acc, _ := state.GetAccount(p1Hex)
	if acc.Balance != 100 {
		t.Fatalf("balance should be unchanged, got %d", acc.Balance)
	}
}

func TestSessionOpen_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	if err := handleSessionOpen(ctx, []byte("invalid json")); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestSessionResult_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	if err := handleSessionResult(ctx, []byte("invalid json")); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestSessionResult_SessionNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "nonexistent",
		Outcome:   map[string]uint64{},
	})

	if err := handleSessionResult(ctx, payload); err == nil {
		t.Fatal("expected error for session not found")
	}
}

func TestSessionCancel_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	if err := handleSessionCancel(ctx, []byte("invalid json")); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestSessionCancel_SessionNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "nonexistent"})

	if err := handleSessionCancel(ctx, payload); err == nil {
		t.Fatal("expected error for session not found")
	}
}

func TestSessionCancel_ZeroStakesRefund(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAccount(&core.Account{Address: pubHex, Balance: 500})
	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  0,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxSessionCancel
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "s1"})

	if err := handleSessionCancel(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	// Balance should be unchanged (no stakes to refund)
	acc, _ := state.GetAccount(pubHex)
	if acc.Balance != 500 {
		t.Fatalf("balance should be unchanged, got %d", acc.Balance)
	}

	sess, _ := state.GetSession("s1")
	if sess.Status != "cancelled" {
		t.Errorf("status = %s, want cancelled", sess.Status)
	}
}

func TestSessionResult_ZeroStakes(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s1",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  0,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxSessionResult
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s1",
		Outcome:   map[string]uint64{}, // zero stakes, zero rewards -> must match
	})

	if err := handleSessionResult(ctx, payload); err != nil {
		t.Fatalf("expected success for zero-stakes result: %v", err)
	}

	sess, _ := state.GetSession("s1")
	if sess.Status != "closed" {
		t.Errorf("status = %s, want closed", sess.Status)
	}
}

func TestSessionOpen_InvalidPlayerPubkey(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub1, _ := crypto.GenerateKeyPair()
	pub1Hex := pub1.Hex()

	ctx := newCtx(t, state, pub1Hex)
	// Use an invalid hex string for a player that is NOT the sender,
	// so the consent-signature check path tries to parse the pubkey.
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID:  "s_invalid_player",
		Players:    []string{pub1Hex, "not-a-valid-pubkey"},
		Signatures: map[string]string{"not-a-valid-pubkey": "deadbeef"},
	})

	err := handleSessionOpen(ctx, payload)
	if err == nil {
		t.Fatal("expected error for invalid player pubkey")
	}
	if got := err.Error(); !strings.Contains(got, "invalid player pubkey") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionResult_TotalStakesOverflow(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	// Create a session where stakes * nPlayers would overflow uint64.
	// Using 2 players with stakes = MaxUint64 causes 2 * MaxUint64 overflow.
	_, pub2, _ := crypto.GenerateKeyPair()
	pub2Hex := pub2.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s_overflow",
		Creator: pubHex,
		Players: []string{pubHex, pub2Hex},
		Stakes:  math.MaxUint64,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxSessionResult
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s_overflow",
		Outcome:   map[string]uint64{pubHex: 100},
	})

	err := handleSessionResult(ctx, payload)
	if err == nil {
		t.Fatal("expected error for total stakes overflow")
	}
	if got := err.Error(); !strings.Contains(got, "total stakes overflow") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionResult_RewardOverflowForPlayer(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	// Session with stakes that produce a large total.
	_ = state.SetSession(&core.Session{
		ID:      "s_roverflow",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  1000,
		Status:  "open",
	})
	// Give the player a balance near MaxUint64 so adding the reward overflows.
	_ = state.SetAccount(&core.Account{Address: pubHex, Balance: math.MaxUint64 - 500})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxSessionResult
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s_roverflow",
		Outcome:   map[string]uint64{pubHex: 1000},
	})

	err := handleSessionResult(ctx, payload)
	if err == nil {
		t.Fatal("expected error for reward overflow")
	}
	if got := err.Error(); !strings.Contains(got, "reward overflow") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionCancel_RefundOverflow(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	// Player balance near MaxUint64; refunding stakes would overflow.
	_ = state.SetAccount(&core.Account{Address: pubHex, Balance: math.MaxUint64 - 10})
	_ = state.SetSession(&core.Session{
		ID:      "s_refund_overflow",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  100,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxSessionCancel
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "s_refund_overflow"})

	err := handleSessionCancel(ctx, payload)
	if err == nil {
		t.Fatal("expected error for refund overflow")
	}
	if got := err.Error(); !strings.Contains(got, "balance overflow on refund") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionOpen_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAccount(&core.Account{Address: pubHex, Balance: 500})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	payload, _ := json.Marshal(core.SessionOpenPayload{
		SessionID: "s_nil_emitter",
		GameID:    "game1",
		Players:   []string{pubHex},
		Stakes:    0,
	})

	if err := handleSessionOpen(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestSessionResult_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s_nil_emitter_r",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  0,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxSessionResult
	payload, _ := json.Marshal(core.SessionResultPayload{
		SessionID: "s_nil_emitter_r",
		Outcome:   map[string]uint64{},
	})

	if err := handleSessionResult(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestSessionCancel_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetSession(&core.Session{
		ID:      "s_nil_emitter_c",
		Creator: pubHex,
		Players: []string{pubHex},
		Stakes:  0,
		Status:  "open",
	})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxSessionCancel
	payload, _ := json.Marshal(core.SessionCancelPayload{SessionID: "s_nil_emitter_c"})

	if err := handleSessionCancel(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

