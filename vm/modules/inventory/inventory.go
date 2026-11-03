package inventory

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func handleEquipItem(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.EquipItemPayload](payload, core.TxEquipItem)
	if err != nil {
		return err
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
	if asset.Listed() {
		return errors.New("cannot equip a listed asset")
	}

	inv, err := ctx.State.GetInventory(ctx.EffectiveSender)
	if err != nil {
		return fmt.Errorf("get inventory: %w", err)
	}
	if existing, ok := inv.Slots[p.Slot]; ok && existing != "" {
		return fmt.Errorf("slot %q already occupied by asset %s", p.Slot, existing)
	}
	if slot, equipped := slotOf(inv, p.AssetID); equipped {
		return fmt.Errorf("asset already equipped in slot %q", slot)
	}

	inv.Slots[p.Slot] = p.AssetID
	if err := ctx.State.SetInventory(inv); err != nil {
		return err
	}
	if asset.Properties == nil {
		asset.Properties = make(map[string]any)
	}
	asset.Properties[core.AssetPropEquipped] = true
	asset.Properties[core.AssetPropSlot] = p.Slot
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	ctx.Emit(events.EventItemEquipped, events.ItemEquippedData{AssetID: p.AssetID, Slot: p.Slot, Owner: ctx.EffectiveSender})
	return nil
}

func handleUnequipItem(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.UnequipItemPayload](payload, core.TxUnequipItem)
	if err != nil {
		return err
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
	slot, equipped := slotOf(inv, p.AssetID)
	if !equipped {
		return errors.New("asset is not equipped")
	}
	delete(inv.Slots, slot)
	if err := ctx.State.SetInventory(inv); err != nil {
		return err
	}

	delete(asset.Properties, core.AssetPropEquipped)
	delete(asset.Properties, core.AssetPropSlot)
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	ctx.Emit(events.EventItemUnequipped, events.ItemUnequippedData{AssetID: p.AssetID, Owner: ctx.EffectiveSender})
	return nil
}

// slotOf finds the slot holding assetID. An asset occupies at most one slot,
// so map iteration order does not matter.
func slotOf(inv *core.Inventory, assetID string) (string, bool) {
	for slot, id := range inv.Slots {
		if id == assetID {
			return slot, true
		}
	}
	return "", false
}
