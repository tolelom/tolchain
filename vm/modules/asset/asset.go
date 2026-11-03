package asset

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func handleMintAsset(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.MintAssetPayload](payload, core.TxMintAsset)
	if err != nil {
		return err
	}
	if p.TemplateID == "" {
		return errors.New("template_id required")
	}
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	tmpl, err := ctx.State.GetTemplate(p.TemplateID)
	if err != nil {
		return fmt.Errorf("template %q not found: %w", p.TemplateID, err)
	}

	owner := p.Owner
	if owner == "" {
		owner = ctx.EffectiveSender
	} else if err := vm.ValidatePubKey(owner, "owner pubkey"); err != nil {
		return err
	}
	if err := ValidateSchema(tmpl.Schema, p.Properties); err != nil {
		return fmt.Errorf("schema validation: %w", err)
	}

	assetID := crypto.Hash([]byte(ctx.Tx.ID + ":asset:" + p.TemplateID))
	asset := &core.Asset{
		ID:         assetID,
		TemplateID: p.TemplateID,
		Owner:      owner,
		Properties: p.Properties,
		Tradeable:  tmpl.Tradeable,
		MintedAt:   ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	ctx.Emit(events.EventAssetMinted, events.AssetMintedData{AssetID: assetID, TemplateID: p.TemplateID, Owner: owner})
	return nil
}

func handleBurnAsset(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.BurnAssetPayload](payload, core.TxBurnAsset)
	if err != nil {
		return err
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.EffectiveSender {
		return errors.New("only the asset owner can burn it")
	}
	if asset.Listed() {
		return fmt.Errorf("asset %q has an active listing; cancel it before burning", p.AssetID)
	}
	if asset.Equipped() {
		return fmt.Errorf("asset %q is equipped; unequip it before burning", p.AssetID)
	}

	if err := ctx.State.DeleteAsset(p.AssetID); err != nil {
		return err
	}

	ctx.Emit(events.EventAssetBurned, events.AssetBurnedData{AssetID: p.AssetID, Owner: asset.Owner})
	return nil
}

func handleTransferAsset(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.TransferAssetPayload](payload, core.TxTransferAsset)
	if err != nil {
		return err
	}
	if err := vm.ValidatePubKey(p.To, "to address"); err != nil {
		return err
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.EffectiveSender {
		return errors.New("only the asset owner can transfer it")
	}
	if !asset.Tradeable {
		return errors.New("asset is not tradeable")
	}
	if asset.Listed() {
		return fmt.Errorf("asset %q has an active listing; cancel it before transferring", p.AssetID)
	}
	if asset.Equipped() {
		return fmt.Errorf("asset %q is equipped; unequip it before transferring", p.AssetID)
	}

	asset.Owner = p.To
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	ctx.Emit(events.EventAssetTransfer, events.AssetTransferData{AssetID: p.AssetID, From: ctx.EffectiveSender, To: p.To})
	return nil
}

// ValidateSchema checks properties against a template schema whose values are
// type hints: "string", "int" or "bool". Properties not in the schema are
// rejected, missing ones are allowed, unknown hints are not checked, and an
// empty schema allows anything.
func ValidateSchema(schema map[string]any, properties map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	for key := range properties {
		if _, ok := schema[key]; !ok {
			return fmt.Errorf("property %q not defined in template schema", key)
		}
	}
	for key, typHint := range schema {
		val, ok := properties[key]
		if !ok {
			continue
		}
		expected, _ := typHint.(string)
		switch expected {
		case "string":
			if _, ok := val.(string); !ok {
				return fmt.Errorf("property %q must be a string", key)
			}
		case "int":
			if !isInteger(val) {
				return fmt.Errorf("property %q must be an integer", key)
			}
		case "bool":
			if _, ok := val.(bool); !ok {
				return fmt.Errorf("property %q must be a bool", key)
			}
		}
	}
	return nil
}

// isInteger accepts JSON numbers (decoded as float64) with no fractional part.
//
// The float64→int64 round trip is implementation-defined for |v| >= 2^63
// (amd64 and arm64 disagree at exactly 2^63); changing it changes which
// blocks are valid, so it is left as is.
func isInteger(val any) bool {
	switch v := val.(type) {
	case float64:
		return v == float64(int64(v))
	case json.Number:
		_, err := v.Int64()
		return err == nil
	default:
		return false
	}
}
