package vm

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// DecodePayload unmarshals a handler's payload into T.
func DecodePayload[T any](payload json.RawMessage, typ core.TxType) (*T, error) {
	var p T
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("decode %s payload: %w", typ, err)
	}
	return &p, nil
}

// SafeAdd returns a + b, or core.ErrBalanceOverflow.
func SafeAdd(a, b uint64) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, core.ErrBalanceOverflow
	}
	return a + b, nil
}

// RequireOperator rejects a sender outside the configured operator set. With
// no operators configured every sender passes.
func RequireOperator(ctx *Context) error {
	if len(ctx.Operators) == 0 {
		return nil
	}
	if !ctx.Operators[ctx.EffectiveSender] {
		return fmt.Errorf("not an operator: %s: %w", ctx.EffectiveSender, core.ErrUnauthorized)
	}
	return nil
}

func ValidatePubKey(hexStr, fieldName string) error {
	if hexStr == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if _, err := crypto.PubKeyFromHex(hexStr); err != nil {
		return fmt.Errorf("invalid %s: %w", fieldName, err)
	}
	return nil
}

// ChargeDelegationAmount counts amount against the delegation's MaxAmount when
// the tx is delegated, and fails once the limit would be exceeded. Handlers
// call it before moving the granter's tokens, so a failure rolls back with the
// tx. MaxAmount 0 means unlimited.
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
