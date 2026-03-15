package economy

import (
	"encoding/json"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxTransfer, handleTransfer)
}

func handleTransfer(ctx *vm.Context, payload json.RawMessage) error {
	var p core.TransferPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode transfer payload: %w", err)
	}
	if p.Amount == 0 {
		return fmt.Errorf("transfer amount must be > 0")
	}
	if err := vm.ValidatePubKey(p.To, "to address"); err != nil {
		return err
	}

	// The sender account is loaded again here because applyTx already saved it
	// with the fee deduction and nonce increment. We need the updated state.
	// This is intentionally a second read from the dirty buffer (O(1) in-memory).
	sender, err := ctx.State.GetAccount(ctx.Tx.From)
	if err != nil {
		return err
	}
	if sender.Balance < p.Amount {
		return fmt.Errorf("insufficient balance: have %d, need %d: %w", sender.Balance, p.Amount, core.ErrInsufficientBalance)
	}
	sender.Balance -= p.Amount
	if err := ctx.State.SetAccount(sender); err != nil {
		return err
	}

	recipient, err := ctx.State.GetAccount(p.To)
	if err != nil {
		return err
	}
	newBal, err := vm.SafeAdd(recipient.Balance, p.Amount)
	if err != nil {
		return fmt.Errorf("recipient balance overflow: %w", err)
	}
	recipient.Balance = newBal
	if err := ctx.State.SetAccount(recipient); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventTokenTransfer,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data: map[string]any{
				"from":   ctx.Tx.From,
				"to":     p.To,
				"amount": p.Amount,
			},
		})
	}
	return nil
}
