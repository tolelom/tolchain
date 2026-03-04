package reward

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxGrantReward, handleGrantReward)
}

func handleGrantReward(ctx *vm.Context, payload json.RawMessage) error {
	if ctx.Operators != nil && !ctx.Operators[ctx.Tx.From] {
		return errors.New("grant_reward requires operator authority")
	}

	var p core.GrantRewardPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode grant_reward payload: %w", err)
	}
	if p.Recipient == "" {
		return errors.New("recipient required")
	}
	if _, err := crypto.PubKeyFromHex(p.Recipient); err != nil {
		return fmt.Errorf("invalid recipient pubkey: %w", err)
	}

	// Credit tokens.
	if p.TokenAmount > 0 {
		acc, err := ctx.State.GetAccount(p.Recipient)
		if err != nil {
			return fmt.Errorf("get recipient account: %w", err)
		}
		if acc.Balance > math.MaxUint64-p.TokenAmount {
			return errors.New("recipient balance overflow")
		}
		acc.Balance += p.TokenAmount
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
