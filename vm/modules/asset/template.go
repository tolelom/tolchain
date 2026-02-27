package asset

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxRegisterTemplate, handleRegisterTemplate)
}

func handleRegisterTemplate(ctx *vm.Context, payload json.RawMessage) error {
	var p core.RegisterTemplatePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode register_template payload: %w", err)
	}
	if p.ID == "" {
		return errors.New("template id required")
	}

	// Prevent overwriting an existing template
	_, err := ctx.State.GetTemplate(p.ID)
	if err == nil {
		return fmt.Errorf("template %q already exists", p.ID)
	}
	if !errors.Is(err, core.ErrNotFound) {
		return fmt.Errorf("check template %q: %w", p.ID, err)
	}

	t := &core.AssetTemplate{
		ID:        p.ID,
		Name:      p.Name,
		Schema:    p.Schema,
		Tradeable: p.Tradeable,
		Creator:   ctx.Tx.From,
	}
	if err := ctx.State.SetTemplate(t); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventTemplateReg,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"template_id": p.ID, "name": p.Name},
		})
	}
	return nil
}
