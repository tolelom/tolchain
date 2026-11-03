package reward

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/vm/modules/asset"
)

const maxRewardAssets = 50

func handleGrantReward(ctx *vm.Context, payload json.RawMessage) error {
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}
	p, err := vm.DecodePayload[core.GrantRewardPayload](payload, core.TxGrantReward)
	if err != nil {
		return err
	}
	if err := vm.ValidatePubKey(p.Recipient, "recipient"); err != nil {
		return err
	}
	if len(p.Assets) > maxRewardAssets {
		return fmt.Errorf("too many assets in reward: max %d, got %d", maxRewardAssets, len(p.Assets))
	}

	if p.TokenAmount > 0 {
		if err := vm.ChargeDelegationAmount(ctx, p.TokenAmount); err != nil {
			return err
		}
		if err := mintTokens(ctx, p.Recipient, p.TokenAmount); err != nil {
			return err
		}
	}
	for i, mint := range p.Assets {
		if err := mintRewardAsset(ctx, p.Recipient, i, mint); err != nil {
			return err
		}
	}

	ctx.Emit(events.EventRewardGranted, events.RewardGrantedData{Recipient: p.Recipient, TokenAmount: p.TokenAmount, AssetCount: len(p.Assets)})
	return nil
}

// mintTokens creates new tokens for recipient. The supply counter is only
// maintained while a cap is configured.
func mintTokens(ctx *vm.Context, recipient string, amount uint64) error {
	if ctx.MaxTotalSupply > 0 {
		supply, err := ctx.State.GetAccount(core.TotalSupplyAddress)
		if err != nil {
			return fmt.Errorf("get total supply: %w", err)
		}
		// Written as a subtraction so supply+amount can never wrap.
		if amount > ctx.MaxTotalSupply || supply.Balance > ctx.MaxTotalSupply-amount {
			return errors.New("total supply cap exceeded")
		}
		supply.Balance += amount
		if err := ctx.State.SetAccount(supply); err != nil {
			return fmt.Errorf("update total supply: %w", err)
		}
	}

	acc, err := ctx.State.GetAccount(recipient)
	if err != nil {
		return fmt.Errorf("get recipient account: %w", err)
	}
	newBal, err := vm.SafeAdd(acc.Balance, amount)
	if err != nil {
		return fmt.Errorf("recipient balance overflow")
	}
	acc.Balance = newBal
	return ctx.State.SetAccount(acc)
}

func mintRewardAsset(ctx *vm.Context, recipient string, i int, mint core.MintAssetPayload) error {
	tmpl, err := ctx.State.GetTemplate(mint.TemplateID)
	if err != nil {
		return fmt.Errorf("asset[%d] template %q not found: %w", i, mint.TemplateID, err)
	}
	if err := asset.ValidateSchema(tmpl.Schema, mint.Properties); err != nil {
		return fmt.Errorf("reward asset %s: schema validation failed: %w", mint.TemplateID, err)
	}
	assetID := crypto.Hash([]byte(fmt.Sprintf("%s:reward:%d", ctx.Tx.ID, i)))
	a := &core.Asset{
		ID:         assetID,
		TemplateID: mint.TemplateID,
		Owner:      recipient,
		Properties: mint.Properties,
		Tradeable:  tmpl.Tradeable,
		MintedAt:   ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetAsset(a); err != nil {
		return fmt.Errorf("set asset[%d]: %w", i, err)
	}
	ctx.Emit(events.EventAssetMinted, events.AssetMintedData{AssetID: assetID, TemplateID: mint.TemplateID, Owner: recipient})
	return nil
}
