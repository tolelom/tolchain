package delegation

import (
	"encoding/json"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxGrantDelegation, handleGrantDelegation)
	vm.Register(core.TxRevokeDelegation, handleRevokeDelegation)
}

func handleGrantDelegation(ctx *vm.Context, payload json.RawMessage) error {
	var p core.GrantDelegationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode grant_delegation payload: %w", err)
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

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventDelegationGranted,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data: map[string]any{
				"granter":     ctx.Tx.From,
				"grantee":     p.Grantee,
				"allow_types": p.AllowTypes,
				"expires_at":  p.ExpiresAt,
			},
		})
	}
	return nil
}

func handleRevokeDelegation(ctx *vm.Context, payload json.RawMessage) error {
	var p core.RevokeDelegationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode revoke_delegation payload: %w", err)
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

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventDelegationRevoked,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data: map[string]any{
				"granter": ctx.Tx.From,
				"grantee": p.Grantee,
			},
		})
	}
	return nil
}
