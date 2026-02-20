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
	if p.To == "" {
		return fmt.Errorf("transfer to address required")
	}

	sender, err := ctx.State.GetAccount(ctx.Tx.From)
	if err != nil {
		return err
	}
	if sender.Balance < p.Amount {
		return fmt.Errorf("insufficient balance: have %d, need %d", sender.Balance, p.Amount)
	}
	sender.Balance -= p.Amount
	if err := ctx.State.SetAccount(sender); err != nil {
		return err
	}

	recipient, err := ctx.State.GetAccount(p.To)
	if err != nil {
		return err
	}
	recipient.Balance += p.Amount
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
