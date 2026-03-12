package economy

import (
	"encoding/json"
	"math"
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
	tx := &core.Transaction{ID: "tx1", From: fromPubHex, Type: core.TxTransfer}
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter}
}

func TestTransfer_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()
	recipientHex := pub2.Hex()

	// Give sender some tokens.
	_ = state.SetAccount(&core.Account{Address: senderHex, Balance: 1000})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: recipientHex, Amount: 300})

	if err := handleTransfer(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	sender, _ := state.GetAccount(senderHex)
	recipient, _ := state.GetAccount(recipientHex)
	if sender.Balance != 700 {
		t.Errorf("sender balance = %d, want 700", sender.Balance)
	}
	if recipient.Balance != 300 {
		t.Errorf("recipient balance = %d, want 300", recipient.Balance)
	}
}

func TestTransfer_ZeroAmount(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: pub2.Hex(), Amount: 0})

	if err := handleTransfer(ctx, payload); err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestTransfer_EmptyTo(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: "", Amount: 100})

	if err := handleTransfer(ctx, payload); err == nil {
		t.Fatal("expected error for empty to")
	}
}

func TestTransfer_InvalidToPubkey(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: "not-a-valid-pubkey", Amount: 100})

	if err := handleTransfer(ctx, payload); err == nil {
		t.Fatal("expected error for invalid to pubkey")
	}
}

func TestTransfer_InsufficientBalance(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	_ = state.SetAccount(&core.Account{Address: senderHex, Balance: 50})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: pub2.Hex(), Amount: 100})

	if err := handleTransfer(ctx, payload); err == nil {
		t.Fatal("expected error for insufficient balance")
	}
}

func TestTransfer_RecipientOverflow(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()
	recipientHex := pub2.Hex()

	_ = state.SetAccount(&core.Account{Address: senderHex, Balance: 100})
	_ = state.SetAccount(&core.Account{Address: recipientHex, Balance: math.MaxUint64 - 50})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: recipientHex, Amount: 100})

	if err := handleTransfer(ctx, payload); err == nil {
		t.Fatal("expected error for recipient balance overflow")
	}
}

func TestTransfer_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())

	if err := handleTransfer(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestTransfer_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, senderPub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	senderHex := senderPub.Hex()
	recipientHex := recipientPub.Hex()

	state.SetAccount(&core.Account{Address: senderHex, Balance: 1000})

	ctx := newCtx(t, state, senderHex)
	ctx.Emitter = nil // nil emitter
	payload, _ := json.Marshal(core.TransferPayload{To: recipientHex, Amount: 100})

	if err := handleTransfer(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestTransfer_ToNewAccount(t *testing.T) {
	state := testutil.NewStateDB()
	_, senderPub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	senderHex := senderPub.Hex()
	recipientHex := recipientPub.Hex()

	state.SetAccount(&core.Account{Address: senderHex, Balance: 500})
	// Don't create recipient account - it should auto-create as zero-value

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: recipientHex, Amount: 200})

	if err := handleTransfer(ctx, payload); err != nil {
		t.Fatalf("expected success: %v", err)
	}

	acc, _ := state.GetAccount(recipientHex)
	if acc.Balance != 200 {
		t.Fatalf("recipient balance = %d, want 200", acc.Balance)
	}
}

func TestTransfer_SelfTransfer(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAccount(&core.Account{Address: pubHex, Balance: 1000})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.TransferPayload{To: pubHex, Amount: 300})

	if err := handleTransfer(ctx, payload); err != nil {
		t.Fatalf("expected success for self-transfer, got %v", err)
	}

	acc, _ := state.GetAccount(pubHex)
	// After self-transfer: sender deducted 300, then recipient (same account) gets 300 added.
	// The sender balance was set to 700, saved. Then recipient is read (700), +300 = 1000.
	if acc.Balance != 1000 {
		t.Errorf("self-transfer balance = %d, want 1000", acc.Balance)
	}
}

func TestTransfer_ExactBalance(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()
	recipientHex := pub2.Hex()

	_ = state.SetAccount(&core.Account{Address: senderHex, Balance: 500})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: recipientHex, Amount: 500})

	if err := handleTransfer(ctx, payload); err != nil {
		t.Fatalf("expected success for exact balance transfer, got %v", err)
	}

	sender, _ := state.GetAccount(senderHex)
	recipient, _ := state.GetAccount(recipientHex)
	if sender.Balance != 0 {
		t.Errorf("sender balance = %d, want 0", sender.Balance)
	}
	if recipient.Balance != 500 {
		t.Errorf("recipient balance = %d, want 500", recipient.Balance)
	}
}

func TestTransfer_RecipientOverflow_Boundary(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()
	recipientHex := pub2.Hex()

	// Recipient at exactly MaxUint64 - 100. Transferring 100 should succeed (no overflow).
	_ = state.SetAccount(&core.Account{Address: senderHex, Balance: 100})
	_ = state.SetAccount(&core.Account{Address: recipientHex, Balance: math.MaxUint64 - 100})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferPayload{To: recipientHex, Amount: 100})

	if err := handleTransfer(ctx, payload); err != nil {
		t.Fatalf("expected success at overflow boundary, got %v", err)
	}

	recipient, _ := state.GetAccount(recipientHex)
	if recipient.Balance != math.MaxUint64 {
		t.Errorf("recipient balance = %d, want MaxUint64", recipient.Balance)
	}
}
