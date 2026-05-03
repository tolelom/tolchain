package vm

import (
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// SafeAdd returns a + b, or an error if the result would overflow uint64.
func SafeAdd(a, b uint64) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, core.ErrBalanceOverflow
	}
	return a + b, nil
}

// RequireOperator returns an error if operators are configured and the
// transaction sender is not among them. When no operators are configured
// (nil or empty map), all senders are permitted.
func RequireOperator(ctx *Context) error {
	if len(ctx.Operators) == 0 {
		return nil
	}
	if !ctx.Operators[ctx.EffectiveSender] {
		return fmt.Errorf("not an operator: %s: %w", ctx.EffectiveSender, core.ErrUnauthorized)
	}
	return nil
}

// ValidatePubKey checks that hexStr is a valid ed25519 public key hex string.
func ValidatePubKey(hexStr, fieldName string) error {
	if hexStr == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if _, err := crypto.PubKeyFromHex(hexStr); err != nil {
		return fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	return nil
}

// ChargeDelegationAmount enforces the MaxAmount limit on a delegation grant
// for tx that move tokens (transfer, grant_reward). Caller passes the amount
// the tx wants to spend on behalf of the granter; this updates SpentAmount
// in state and rejects if MaxAmount would be exceeded.
//
// No-op when tx is direct (OnBehalfOf == ""), grant has MaxAmount == 0
// (unlimited), or amount == 0.
//
// Must be called from inside a tx handler so failures roll back via the
// executor's per-tx snapshot.
func ChargeDelegationAmount(ctx *Context, amount uint64) error {
	if amount == 0 || ctx.Tx.OnBehalfOf == "" {
		return nil
	}
	grant, err := ctx.State.GetDelegation(ctx.Tx.OnBehalfOf, ctx.Tx.From)
	if err != nil {
		return fmt.Errorf("delegation amount check: get grant: %w", err)
	}
	if grant.MaxAmount == 0 {
		return nil
	}
	newSpent, err := SafeAdd(grant.SpentAmount, amount)
	if err != nil {
		return fmt.Errorf("delegation spent_amount overflow: %w", err)
	}
	if newSpent > grant.MaxAmount {
		return fmt.Errorf("delegation max_amount exceeded: would spend %d, limit %d (already spent %d): %w",
			amount, grant.MaxAmount, grant.SpentAmount, core.ErrUnauthorized)
	}
	grant.SpentAmount = newSpent
	if err := ctx.State.SetDelegation(grant); err != nil {
		return fmt.Errorf("delegation amount check: save grant: %w", err)
	}
	return nil
}
