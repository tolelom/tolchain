package inventory

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxEquipItem, handleEquipItem)
	vm.Register(core.TxUnequipItem, handleUnequipItem)
}

func handleEquipItem(ctx *vm.Context, payload json.RawMessage) error {
	var p core.EquipItemPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode equip_item payload: %w", err)
	}
	if p.AssetID == "" || p.Slot == "" {
		return errors.New("asset_id and slot required")
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.EffectiveSender {
		return errors.New("not the asset owner")
	}
	if asset.ActiveListingID != "" {
		return errors.New("cannot equip a listed asset")
	}

	inv, err := ctx.State.GetInventory(ctx.EffectiveSender)
	if err != nil {
		return fmt.Errorf("get inventory: %w", err)
	}

	// Check if slot already occupied.
	if existing, ok := inv.Slots[p.Slot]; ok && existing != "" {
		return fmt.Errorf("slot %q already occupied by asset %s", p.Slot, existing)
	}

	// Check if asset is already equipped in another slot.
	for slot, aid := range inv.Slots {
		if aid == p.AssetID {
			return fmt.Errorf("asset already equipped in slot %q", slot)
		}
	}

	inv.Slots[p.Slot] = p.AssetID
	if err := ctx.State.SetInventory(inv); err != nil {
		return err
	}

	if asset.Properties == nil {
		asset.Properties = make(map[string]any)
	}
	asset.Properties["equipped"] = true
	asset.Properties["slot"] = p.Slot
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventItemEquipped,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"asset_id": p.AssetID, "slot": p.Slot, "owner": ctx.EffectiveSender},
		})
	}
	return nil
}

func handleUnequipItem(ctx *vm.Context, payload json.RawMessage) error {
	var p core.UnequipItemPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode unequip_item payload: %w", err)
	}
	if p.AssetID == "" {
		return errors.New("asset_id required")
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.EffectiveSender {
		return errors.New("not the asset owner")
	}

	inv, err := ctx.State.GetInventory(ctx.EffectiveSender)
	if err != nil {
		return fmt.Errorf("get inventory: %w", err)
	}

	found := false
	for slot, aid := range inv.Slots {
		if aid == p.AssetID {
			delete(inv.Slots, slot)
			found = true
			break
		}
	}
	if !found {
		return errors.New("asset is not equipped")
	}

	if err := ctx.State.SetInventory(inv); err != nil {
		return err
	}

	delete(asset.Properties, "equipped")
	delete(asset.Properties, "slot")
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventItemUnequipped,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"asset_id": p.AssetID, "owner": ctx.EffectiveSender},
		})
	}
	return nil
}
