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

	tmpl, err := ctx.State.GetTemplate(p.TemplateID)
	if err != nil {
		return fmt.Errorf("template %q not found: %w", p.TemplateID, err)
	}

	owner := p.Owner
	if owner == "" {
		owner = ctx.Tx.From
	} else {
		// Validate that the provided owner is a real ed25519 pubkey.
		if _, err := crypto.PubKeyFromHex(owner); err != nil {
			return fmt.Errorf("invalid owner pubkey: %w", err)
		}
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
	if p.To == "" {
		return errors.New("to address required")
	}
	// Validate recipient is a real ed25519 pubkey.
	if _, err := crypto.PubKeyFromHex(p.To); err != nil {
		return fmt.Errorf("invalid to pubkey: %w", err)
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
