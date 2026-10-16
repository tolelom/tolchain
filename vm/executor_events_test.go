package vm

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
)

var (
	testTxTypeEmitThenFail = core.TxType("__test_emit_then_fail__")
	testTxTypeEmitThenOK   = core.TxType("__test_emit_then_ok__")
	emitHandlersOnce       sync.Once
)

func registerEmitHandlers() {
	emitHandlersOnce.Do(func() {
		globalRegistry.Register(testTxTypeEmitThenFail, func(ctx *Context, payload json.RawMessage) error {
			ctx.Emitter.Emit(events.Event{Type: events.EventAssetMinted, TxID: ctx.Tx.ID})
			return errors.New("fails after emitting")
		})
		globalRegistry.Register(testTxTypeEmitThenOK, func(ctx *Context, payload json.RawMessage) error {
			ctx.Emitter.Emit(events.Event{Type: events.EventAssetMinted, TxID: ctx.Tx.ID})
			return nil
		})
	})
}

func TestEvents_FailedTxDropsHandlerEvents(t *testing.T) {
	registerEmitHandlers()
	state := testutil.NewStateDB()
	em := &eventCollector{}
	exec := NewExecutor(state, em)
	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000})

	tx := signedTx(t, priv, pubHex, 0, 0, testTxTypeEmitThenFail)
	if err := exec.ExecuteTx(makeBlock(pubHex, tx), tx); err == nil {
		t.Fatal("expected handler failure")
	}

	evts := em.get()
	if len(evts) != 1 || evts[0].Type != events.EventTxExecuted || evts[0].Data["success"] != false {
		t.Fatalf("a failed tx must leave only its failure event, got %+v", evts)
	}
}

func TestEvents_SuccessfulTxKeepsHandlerEventsBeforeResult(t *testing.T) {
	registerEmitHandlers()
	state := testutil.NewStateDB()
	em := &eventCollector{}
	exec := NewExecutor(state, em)
	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000})

	tx := signedTx(t, priv, pubHex, 0, 0, testTxTypeEmitThenOK)
	if err := exec.ExecuteTx(makeBlock(pubHex, tx), tx); err != nil {
		t.Fatal(err)
	}

	evts := em.get()
	if len(evts) != 2 || evts[0].Type != events.EventAssetMinted || evts[1].Type != events.EventTxExecuted {
		t.Fatalf("want [asset_minted, tx_executed], got %+v", evts)
	}
}

var (
	testTxTypePanic = core.TxType("__test_panic__")
	panicOnce       sync.Once
)

func TestExecuteTx_HandlerPanicFailsTxAndRollsBack(t *testing.T) {
	registerTestHandlers()
	panicOnce.Do(func() {
		globalRegistry.Register(testTxTypePanic, func(ctx *Context, payload json.RawMessage) error {
			ctx.State.SetAccount(&core.Account{Address: "touched", Balance: 1})
			panic("handler bug")
		})
	})
	state := testutil.NewStateDB()
	exec := NewExecutor(state, &eventCollector{})
	priv, pubHex := genKey(t)
	state.SetAccount(&core.Account{Address: pubHex, Balance: 1000})

	tx := signedTx(t, priv, pubHex, 0, 0, testTxTypePanic)
	if err := exec.ExecuteBlock(makeBlock(pubHex, tx)); err == nil {
		t.Fatal("a panicking handler must fail the tx")
	}
	if acc, _ := state.GetAccount("touched"); acc.Balance != 0 {
		t.Fatal("writes made before the panic must be rolled back")
	}
	if acc, _ := state.GetAccount(pubHex); acc.Nonce != 0 {
		t.Fatal("nonce must be rolled back")
	}
	done := make(chan struct{})
	go func() {
		exec.ExecuteTx(makeBlock(pubHex), signedTx(t, priv, pubHex, 0, 0, testTxType))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("executor stayed locked after a handler panic")
	}
}
