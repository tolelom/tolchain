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

func init() {
	vm.Register(core.TxMintAsset, handleMintAsset)
	vm.Register(core.TxBurnAsset, handleBurnAsset)
	vm.Register(core.TxTransferAsset, handleTransferAsset)
}

func handleMintAsset(ctx *vm.Context, payload json.RawMessage) error {
	var p core.MintAssetPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode mint_asset payload: %w", err)
	}
	if p.TemplateID == "" {
		return errors.New("template_id required")
	}

	// Operator restriction: only authorised operators can mint assets.
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	tmpl, err := ctx.State.GetTemplate(p.TemplateID)
	if err != nil {
		return fmt.Errorf("template %q not found: %w", p.TemplateID, err)
	}

	owner := p.Owner
	if owner == "" {
		owner = ctx.Tx.From
	} else {
		// Validate that the provided owner is a real ed25519 pubkey.
		if err := vm.ValidatePubKey(owner, "owner pubkey"); err != nil {
			return err
		}
	}

	// Validate properties against template schema.
	if err := ValidateSchema(tmpl.Schema, p.Properties); err != nil {
		return fmt.Errorf("schema validation: %w", err)
	}

	// Deterministic asset ID: hash of tx ID + template
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

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventAssetMinted,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"asset_id": assetID, "template_id": p.TemplateID, "owner": owner},
		})
	}
	return nil
}

func handleBurnAsset(ctx *vm.Context, payload json.RawMessage) error {
	var p core.BurnAssetPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode burn_asset payload: %w", err)
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.Tx.From {
		return errors.New("only the asset owner can burn it")
	}
	if asset.ActiveListingID != "" {
		return fmt.Errorf("asset %q has an active listing; cancel it before burning", p.AssetID)
	}
	if equipped, _ := asset.Properties["equipped"].(bool); equipped {
		return fmt.Errorf("asset %q is equipped; unequip it before burning", p.AssetID)
	}

	if err := ctx.State.DeleteAsset(p.AssetID); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventAssetBurned,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"asset_id": p.AssetID, "owner": asset.Owner},
		})
	}
	return nil
}

func handleTransferAsset(ctx *vm.Context, payload json.RawMessage) error {
	var p core.TransferAssetPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode transfer_asset payload: %w", err)
	}
	if err := vm.ValidatePubKey(p.To, "to address"); err != nil {
		return err
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.Tx.From {
		return errors.New("only the asset owner can transfer it")
	}
	if !asset.Tradeable {
		return errors.New("asset is not tradeable")
	}
	if asset.ActiveListingID != "" {
		return fmt.Errorf("asset %q has an active listing; cancel it before transferring", p.AssetID)
	}
	if equipped, _ := asset.Properties["equipped"].(bool); equipped {
		return fmt.Errorf("asset %q is equipped; unequip it before transferring", p.AssetID)
	}

	asset.Owner = p.To
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventAssetTransfer,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"asset_id": p.AssetID, "from": ctx.Tx.From, "to": p.To},
		})
	}
	return nil
}

// ValidateSchema checks that properties conform to the template schema.
// Supported schema types: "string", "int", "bool". Extra properties not in
// the schema are rejected. An empty/nil schema allows any properties.
func ValidateSchema(schema map[string]any, properties map[string]any) error {
	if len(schema) == 0 {
		return nil
	}
	// Reject properties not defined in the schema.
	for key := range properties {
		if _, ok := schema[key]; !ok {
			return fmt.Errorf("property %q not defined in template schema", key)
		}
	}
	// Validate types for each schema field.
	for key, typHint := range schema {
		val, ok := properties[key]
		if !ok {
			continue // missing properties are optional
		}
		expected, _ := typHint.(string)
		switch expected {
		case "string":
			if _, ok := val.(string); !ok {
				return fmt.Errorf("property %q must be a string", key)
			}
		case "int":
			// JSON numbers decode as float64.
			switch v := val.(type) {
			case float64:
				if v != float64(int64(v)) {
					return fmt.Errorf("property %q must be an integer", key)
				}
			case json.Number:
				if _, err := v.Int64(); err != nil {
					return fmt.Errorf("property %q must be an integer", key)
				}
			default:
				return fmt.Errorf("property %q must be an integer", key)
			}
		case "bool":
			if _, ok := val.(bool); !ok {
				return fmt.Errorf("property %q must be a bool", key)
			}
		default:
			// Unknown schema type hint — skip validation for this field.
		}
	}
	return nil
}
