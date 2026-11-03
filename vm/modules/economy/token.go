package economy

import (
	"encoding/json"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func handleTransfer(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.TransferPayload](payload, core.TxTransfer)
	if err != nil {
		return err
	}
	if p.Amount == 0 {
		return fmt.Errorf("transfer amount must be > 0")
	}
	if err := vm.ValidatePubKey(p.To, "to address"); err != nil {
		return err
	}
	if err := vm.ChargeDelegationAmount(ctx, p.Amount); err != nil {
		return err
	}

	// Reloaded: the executor already saved the fee and nonce change.
	sender, err := ctx.State.GetAccount(ctx.EffectiveSender)
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

	ctx.Emit(events.EventTokenTransfer, events.TokenTransferData{From: ctx.EffectiveSender, To: p.To, Amount: p.Amount})
	return nil
}
