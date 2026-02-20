package vm

import (
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
)

// Context is passed to every Handler and provides access to the chain state,
// the current block, the triggering transaction, and the event emitter.
type Context struct {
	State   core.State
	Block   *core.Block
	Tx      *core.Transaction
	Emitter *events.Emitter
}

// Executor applies transactions to the state using the global Handler registry.
type Executor struct {
	state   core.State
	emitter *events.Emitter
}

// NewExecutor creates an Executor with the given state and event emitter.
func NewExecutor(state core.State, emitter *events.Emitter) *Executor {
	return &Executor{state: state, emitter: emitter}
}

// ExecuteBlock applies all transactions in block sequentially.
// A failing transaction causes the whole block to be rejected.
// EventBlockCommit is emitted by the caller (consensus) after signing so
// the event carries the correct block hash.
func (e *Executor) ExecuteBlock(block *core.Block) error {
	for _, tx := range block.Transactions {
		if err := e.ExecuteTx(block, tx); err != nil {
			return fmt.Errorf("tx %s failed: %w", tx.ID, err)
		}
	}
	return nil
}

// ExecuteTx verifies and executes a single transaction with snapshot/rollback.
func (e *Executor) ExecuteTx(block *core.Block, tx *core.Transaction) error {
	if err := tx.Verify(); err != nil {
		return fmt.Errorf("signature: %w", err)
	}

	snapID, err := e.state.Snapshot()
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	if err := e.applyTx(block, tx); err != nil {
		if revertErr := e.state.RevertToSnapshot(snapID); revertErr != nil {
			return fmt.Errorf("revert snapshot after tx failure: %w (revert: %v)", err, revertErr)
		}
		return err
	}

	if e.emitter != nil {
		e.emitter.Emit(events.Event{
			Type:        events.EventTxExecuted,
			TxID:        tx.ID,
			BlockHeight: block.Header.Height,
			Data:        map[string]any{"type": string(tx.Type), "from": tx.From},
		})
	}
	return nil
}

// applyTx deducts the fee, increments the nonce, then dispatches to the handler.
func (e *Executor) applyTx(block *core.Block, tx *core.Transaction) error {
	acc, err := e.state.GetAccount(tx.From)
	if err != nil {
		return fmt.Errorf("get account: %w", err)
	}
	if acc.Nonce != tx.Nonce {
		return fmt.Errorf("invalid nonce: expected %d got %d", acc.Nonce, tx.Nonce)
	}
	if acc.Balance < tx.Fee {
		return fmt.Errorf("insufficient balance for fee: have %d need %d", acc.Balance, tx.Fee)
	}
	if acc.Nonce == math.MaxUint64 {
		return fmt.Errorf("nonce overflow for account %s", tx.From)
	}
	acc.Balance -= tx.Fee
	acc.Nonce++
	if err := e.state.SetAccount(acc); err != nil {
		return err
	}

	ctx := &Context{
		State:   e.state,
		Block:   block,
		Tx:      tx,
		Emitter: e.emitter,
	}
	return globalRegistry.Execute(tx.Type, ctx, tx.Payload)
}
