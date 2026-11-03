package asset

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func handleRegisterTemplate(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.RegisterTemplatePayload](payload, core.TxRegisterTemplate)
	if err != nil {
		return err
	}
	if p.ID == "" {
		return errors.New("template id required")
	}
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	_, err = ctx.State.GetTemplate(p.ID)
	if err == nil {
		return fmt.Errorf("template %q already exists: %w", p.ID, core.ErrAlreadyExists)
	}
	if !errors.Is(err, core.ErrNotFound) {
		return fmt.Errorf("check template %q: %w", p.ID, err)
	}

	t := &core.AssetTemplate{
		ID:        p.ID,
		Name:      p.Name,
		Schema:    p.Schema,
		Tradeable: p.Tradeable,
		Creator:   ctx.EffectiveSender,
	}
	if err := ctx.State.SetTemplate(t); err != nil {
		return err
	}

	ctx.Emit(events.EventTemplateRegistered, events.TemplateRegisteredData{TemplateID: p.ID, Name: p.Name})
	return nil
}
