package vm

import (
	"errors"
	"testing"

	"github.com/tolelom/tolchain/core"
)

func TestRequireOperator_RejectsWhenNoOperatorsConfigured(t *testing.T) {
	ctx := &Context{EffectiveSender: "aa"}
	if err := RequireOperator(ctx); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("with no operators configured nobody may act as operator, got %v", err)
	}
}
