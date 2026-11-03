package delegation

import (
	"encoding/json"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

// Delegation txs are signed by the granter itself (Tx.From); the executor
// refuses to run them on someone's behalf.

func handleGrantDelegation(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.GrantDelegationPayload](payload, core.TxGrantDelegation)
	if err != nil {
		return err
	}
	if err := vm.ValidatePubKey(p.Grantee, "grantee"); err != nil {
		return err
	}
	if p.Grantee == ctx.Tx.From {
		return fmt.Errorf("cannot delegate to self")
	}
	if len(p.AllowTypes) == 0 {
		return fmt.Errorf("allow_types must not be empty")
	}
	for _, t := range p.AllowTypes {
		if core.TxType(t) == core.TxGrantDelegation || core.TxType(t) == core.TxRevokeDelegation {
			return fmt.Errorf("cannot delegate %s", t)
		}
	}

	grant := core.DelegationGrant{
		Granter:    ctx.Tx.From,
		Grantee:    p.Grantee,
		AllowTypes: p.AllowTypes,
		ExpiresAt:  p.ExpiresAt,
		MaxUses:    p.MaxUses,
		MaxAmount:  p.MaxAmount,
		CreatedAt:  ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetDelegation(&grant); err != nil {
		return fmt.Errorf("store delegation grant: %w", err)
	}

	ctx.Emit(events.EventDelegationGranted, events.DelegationGrantedData{
		Granter:    ctx.Tx.From,
		Grantee:    p.Grantee,
		AllowTypes: p.AllowTypes,
		ExpiresAt:  p.ExpiresAt,
	})
	return nil
}

func handleRevokeDelegation(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.RevokeDelegationPayload](payload, core.TxRevokeDelegation)
	if err != nil {
		return err
	}
	if err := vm.ValidatePubKey(p.Grantee, "grantee"); err != nil {
		return err
	}
	if _, err := ctx.State.GetDelegation(ctx.Tx.From, p.Grantee); err != nil {
		return fmt.Errorf("delegation grant not found: %w", err)
	}
	if err := ctx.State.DeleteDelegation(ctx.Tx.From, p.Grantee); err != nil {
		return fmt.Errorf("delete delegation grant: %w", err)
	}

	ctx.Emit(events.EventDelegationRevoked, events.DelegationRevokedData{Granter: ctx.Tx.From, Grantee: p.Grantee})
	return nil
}
