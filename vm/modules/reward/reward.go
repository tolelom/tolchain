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

func init() {
	vm.Register(core.TxGrantReward, handleGrantReward)
}

// NOTE: Asset properties are validated against the template schema via
// asset.ValidateSchema. Max asset count is capped at 50. The operator
// signature requirement provides additional access control.
func handleGrantReward(ctx *vm.Context, payload json.RawMessage) error {
	if err := vm.RequireOperator(ctx); err != nil {
		return err
	}

	var p core.GrantRewardPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode grant_reward payload: %w", err)
	}
	if err := vm.ValidatePubKey(p.Recipient, "recipient"); err != nil {
		return err
	}

	const maxRewardAssets = 50
	if len(p.Assets) > maxRewardAssets {
		return fmt.Errorf("too many assets in reward: max %d, got %d", maxRewardAssets, len(p.Assets))
	}

	// Credit tokens.
	if p.TokenAmount > 0 {
		// Enforce delegation MaxAmount on the granter for reward tokens.
		if err := vm.ChargeDelegationAmount(ctx, p.TokenAmount); err != nil {
			return err
		}
		// Enforce total supply cap if configured.
		if ctx.MaxTotalSupply > 0 {
			supply, err := ctx.State.GetAccount("system:total_supply")
			if err != nil {
				return fmt.Errorf("get total supply: %w", err)
			}
			if supply.Balance > ctx.MaxTotalSupply-p.TokenAmount || supply.Balance+p.TokenAmount > ctx.MaxTotalSupply {
				return errors.New("total supply cap exceeded")
			}
			supply.Balance += p.TokenAmount
			if err := ctx.State.SetAccount(supply); err != nil {
				return fmt.Errorf("update total supply: %w", err)
			}
		}

		acc, err := ctx.State.GetAccount(p.Recipient)
		if err != nil {
			return fmt.Errorf("get recipient account: %w", err)
		}
		newBal, err := vm.SafeAdd(acc.Balance, p.TokenAmount)
		if err != nil {
			return fmt.Errorf("recipient balance overflow")
		}
		acc.Balance = newBal
		if err := ctx.State.SetAccount(acc); err != nil {
			return err
		}
	}

	// Mint each asset.
	for i, mintP := range p.Assets {
		tmpl, err := ctx.State.GetTemplate(mintP.TemplateID)
		if err != nil {
			return fmt.Errorf("asset[%d] template %q not found: %w", i, mintP.TemplateID, err)
		}
		if err := asset.ValidateSchema(tmpl.Schema, mintP.Properties); err != nil {
			return fmt.Errorf("reward asset %s: schema validation failed: %w", mintP.TemplateID, err)
		}
		assetID := crypto.Hash([]byte(fmt.Sprintf("%s:reward:%d", ctx.Tx.ID, i)))
		asset := &core.Asset{
			ID:         assetID,
			TemplateID: mintP.TemplateID,
			Owner:      p.Recipient,
			Properties: mintP.Properties,
			Tradeable:  tmpl.Tradeable,
			MintedAt:   ctx.Block.Header.Timestamp,
		}
		if err := ctx.State.SetAsset(asset); err != nil {
			return fmt.Errorf("set asset[%d]: %w", i, err)
		}
		if ctx.Emitter != nil {
			ctx.Emitter.Emit(events.Event{
				Type:        events.EventAssetMinted,
				TxID:        ctx.Tx.ID,
				BlockHeight: ctx.Block.Header.Height,
				Data:        map[string]any{"asset_id": assetID, "template_id": mintP.TemplateID, "owner": p.Recipient},
			})
		}
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventRewardGranted,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"recipient": p.Recipient, "token_amount": p.TokenAmount, "asset_count": len(p.Assets)},
		})
	}
	return nil
}
