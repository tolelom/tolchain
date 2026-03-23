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
