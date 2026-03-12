package vm

import (
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
)

// Test-only tx types that won't conflict with module init() registrations.
var (
	testTxType     = core.TxType("__test_executor__")
	testTxTypeFail = core.TxType("__test_executor_fail__")
	initOnce       sync.Once
)

func registerTestHandlers() {
	initOnce.Do(func() {
		globalRegistry.Register(testTxType, func(ctx *Context, payload json.RawMessage) error {
			return nil // no-op handler
		})
		globalRegistry.Register(testTxTypeFail, func(ctx *Context, payload json.RawMessage) error {
			return errors.New("intentional failure")
		})
	})
}

// helper: generate a key pair and return priv, pubHex
func genKey(t *testing.T) (crypto.PrivateKey, string) {
	t.Helper()
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	return priv, pub.Hex()
}

// helper: create a signed test transaction
func signedTx(t *testing.T, priv crypto.PrivateKey, pubHex string, nonce, fee uint64, txType core.TxType) *core.Transaction {
	t.Helper()
	tx, err := core.NewTransaction("test", txType, pubHex, nonce, fee, map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("NewTransaction: %v", err)
	}
	tx.Sign(priv)
	return tx
}

// helper: create a block with given proposer and transactions
func makeBlock(proposer string, txs ...*core.Transaction) *core.Block {
	return &core.Block{
		Header: core.BlockHeader{
			Height:    1,
			Proposer:  proposer,
			ChainID:   "test",
			Timestamp: time.Now().UnixNano(),
		},
		Transactions: txs,
	}
}

// eventCollector is a minimal EventEmitter that records emitted events.
type eventCollector struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *eventCollector) Emit(ev events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
}

func (c *eventCollector) get() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]events.Event, len(c.events))
	copy(cp, c.events)
	return cp
}

// ─── Tests ───

func TestExecuteTx_ValidTx(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	em := &eventCollector{}
	exec := NewExecutor(state, em)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 10, testTxType)
	block := makeBlock(pubHex, tx) // sender IS proposer

	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("ExecuteTx: %v", err)
	}

	acc, _ := state.GetAccount(pubHex)
	if acc.Nonce != 1 {
		t.Fatalf("expected nonce 1, got %d", acc.Nonce)
	}
	// sender == proposer, so fee cancels out: balance should be 1000
	if acc.Balance != 1000 {
		t.Fatalf("expected balance 1000 (fee cancels), got %d", acc.Balance)
	}
}

func TestExecuteTx_WrongNonce(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 5, 10, testTxType) // nonce mismatch
	block := makeBlock(pubHex, tx)

	err := exec.ExecuteTx(block, tx)
	if err == nil {
		t.Fatal("expected error for wrong nonce")
	}

	// Verify state was rolled back: nonce and balance unchanged
	acc, _ := state.GetAccount(pubHex)
	if acc.Nonce != 0 {
		t.Fatalf("expected nonce 0 after rollback, got %d", acc.Nonce)
	}
	if acc.Balance != 1000 {
		t.Fatalf("expected balance 1000 after rollback, got %d", acc.Balance)
	}
}

func TestExecuteTx_InsufficientBalance(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 5, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 100, testTxType) // fee > balance
	block := makeBlock(pubHex, tx)

	err := exec.ExecuteTx(block, tx)
	if err == nil {
		t.Fatal("expected error for insufficient balance")
	}

	acc, _ := state.GetAccount(pubHex)
	if acc.Balance != 5 {
		t.Fatalf("expected balance 5 after rollback, got %d", acc.Balance)
	}
}

func TestExecuteTx_InvalidSignature(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 10, testTxType)
	tx.Signature = "deadbeef" + tx.Signature[8:] // corrupt signature

	block := makeBlock(pubHex, tx)
	err := exec.ExecuteTx(block, tx)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestExecuteTx_FeeToProposer(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	senderPriv, senderPub := genKey(t)
	_, proposerPub := genKey(t)

	state.SetAccount(&core.Account{Address: senderPub, Balance: 1000, Nonce: 0})
	state.SetAccount(&core.Account{Address: proposerPub, Balance: 500, Nonce: 0})

	tx := signedTx(t, senderPriv, senderPub, 0, 50, testTxType)
	block := makeBlock(proposerPub, tx) // proposer != sender

	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("ExecuteTx: %v", err)
	}

	senderAcc, _ := state.GetAccount(senderPub)
	proposerAcc, _ := state.GetAccount(proposerPub)

	if senderAcc.Balance != 950 {
		t.Fatalf("sender balance: got %d, want 950", senderAcc.Balance)
	}
	if proposerAcc.Balance != 550 {
		t.Fatalf("proposer balance: got %d, want 550", proposerAcc.Balance)
	}
	if senderAcc.Nonce != 1 {
		t.Fatalf("sender nonce: got %d, want 1", senderAcc.Nonce)
	}
}

func TestExecuteTx_SenderIsProposer(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 50, testTxType)
	block := makeBlock(pubHex, tx) // sender == proposer

	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("ExecuteTx: %v", err)
	}

	acc, _ := state.GetAccount(pubHex)
	// Fee deducted then credited back to same account: net zero change
	if acc.Balance != 1000 {
		t.Fatalf("balance: got %d, want 1000 (fee cancels out)", acc.Balance)
	}
	if acc.Nonce != 1 {
		t.Fatalf("nonce: got %d, want 1", acc.Nonce)
	}
}

func TestExecuteBlock_MultipleTxs(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx1 := signedTx(t, priv, pubHex, 0, 10, testTxType)
	tx2 := signedTx(t, priv, pubHex, 1, 20, testTxType)
	block := makeBlock(pubHex, tx1, tx2)

	if err := exec.ExecuteBlock(block); err != nil {
		t.Fatalf("ExecuteBlock: %v", err)
	}

	acc, _ := state.GetAccount(pubHex)
	if acc.Nonce != 2 {
		t.Fatalf("nonce: got %d, want 2", acc.Nonce)
	}
	// sender == proposer, fees cancel out
	if acc.Balance != 1000 {
		t.Fatalf("balance: got %d, want 1000", acc.Balance)
	}
}

func TestExecuteBlock_FailsIfAnyTxFails(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx1 := signedTx(t, priv, pubHex, 0, 10, testTxType)
	tx2 := signedTx(t, priv, pubHex, 1, 10, testTxTypeFail) // handler returns error
	block := makeBlock(pubHex, tx1, tx2)

	err := exec.ExecuteBlock(block)
	if err == nil {
		t.Fatal("expected ExecuteBlock to fail")
	}
}

func TestSetOperators(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	ops := []string{"operator1", "operator2"}
	exec.SetOperators(ops)

	if exec.operators == nil {
		t.Fatal("operators should not be nil")
	}
	if !exec.operators["operator1"] || !exec.operators["operator2"] {
		t.Fatal("operators not set correctly")
	}
	if exec.operators["operator3"] {
		t.Fatal("unexpected operator")
	}

	// Setting empty clears
	exec.SetOperators(nil)
	if exec.operators != nil {
		t.Fatal("operators should be nil after clearing")
	}
}

func TestSetEmitter(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	em1 := &eventCollector{}
	exec := NewExecutor(state, em1)

	em2 := &eventCollector{}
	exec.SetEmitter(em2)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 10, testTxType)
	block := makeBlock(pubHex, tx)

	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("ExecuteTx: %v", err)
	}

	if len(em1.get()) != 0 {
		t.Fatal("old emitter should not have received events")
	}
	if len(em2.get()) == 0 {
		t.Fatal("new emitter should have received events")
	}
}

func TestEvents_EmittedOnSuccess(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	em := &eventCollector{}
	exec := NewExecutor(state, em)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 10, testTxType)
	block := makeBlock(pubHex, tx)

	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("ExecuteTx: %v", err)
	}

	evts := em.get()
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	ev := evts[0]
	if ev.Type != events.EventTxExecuted {
		t.Fatalf("event type: got %s, want %s", ev.Type, events.EventTxExecuted)
	}
	if ev.Data["success"] != true {
		t.Fatalf("event should indicate success, got %v", ev.Data["success"])
	}
	if ev.TxID != tx.ID {
		t.Fatalf("event TxID: got %s, want %s", ev.TxID, tx.ID)
	}
}

func TestEvents_EmittedOnFailure(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	em := &eventCollector{}
	exec := NewExecutor(state, em)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 10, testTxTypeFail)
	block := makeBlock(pubHex, tx)

	err := exec.ExecuteTx(block, tx)
	if err == nil {
		t.Fatal("expected error from failing handler")
	}

	evts := em.get()
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	ev := evts[0]
	if ev.Type != events.EventTxExecuted {
		t.Fatalf("event type: got %s, want %s", ev.Type, events.EventTxExecuted)
	}
	if ev.Data["success"] != false {
		t.Fatalf("event should indicate failure, got %v", ev.Data["success"])
	}
	errMsg, _ := ev.Data["error"].(string)
	if errMsg == "" {
		t.Fatal("event error message should not be empty on failure")
	}
}

func TestExecuteTx_HandlerFailure_StateRolledBack(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	em := &eventCollector{}
	exec := NewExecutor(state, em)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	tx := signedTx(t, priv, pubHex, 0, 10, testTxTypeFail)
	block := makeBlock(pubHex, tx)

	err := exec.ExecuteTx(block, tx)
	if err == nil {
		t.Fatal("expected error from failing handler")
	}

	// State should be rolled back: nonce and balance unchanged
	acc, _ := state.GetAccount(pubHex)
	if acc.Nonce != 0 {
		t.Fatalf("nonce after rollback: got %d, want 0", acc.Nonce)
	}
	if acc.Balance != 1000 {
		t.Fatalf("balance after rollback: got %d, want 1000", acc.Balance)
	}
}

func TestExecuteTx_NonceOverflow(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: math.MaxUint64})

	tx := signedTx(t, priv, pubHex, math.MaxUint64, 0, testTxType)
	block := makeBlock(pubHex, tx)

	err := exec.ExecuteTx(block, tx)
	if err == nil {
		t.Fatal("expected nonce overflow error")
	}
}

func TestExecuteTx_ProposerBalanceOverflow(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	senderPriv, senderPub := genKey(t)
	state.SetAccount(&core.Account{Address: senderPub, Balance: 1000, Nonce: 0})

	_, proposerPub := genKey(t)
	state.SetAccount(&core.Account{Address: proposerPub, Balance: math.MaxUint64, Nonce: 0})

	tx := signedTx(t, senderPriv, senderPub, 0, 10, testTxType)
	block := makeBlock(proposerPub, tx)

	err := exec.ExecuteTx(block, tx)
	if err == nil {
		t.Fatal("expected proposer balance overflow error")
	}
}

func TestExecuteTx_ZeroFeeWithProposer(t *testing.T) {
	registerTestHandlers()
	state := testutil.NewStateDB()
	exec := NewExecutor(state, nil)

	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000, Nonce: 0})

	_, proposerPub := genKey(t)

	tx := signedTx(t, priv, pubHex, 0, 0, testTxType)
	block := makeBlock(proposerPub, tx)

	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("expected success for zero fee: %v", err)
	}

	acc, _ := state.GetAccount(pubHex)
	if acc.Nonce != 1 {
		t.Fatalf("nonce = %d, want 1", acc.Nonce)
	}
	if acc.Balance != 1000 {
		t.Fatalf("balance = %d, want 1000", acc.Balance)
	}
}
