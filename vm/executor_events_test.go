package vm

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

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
