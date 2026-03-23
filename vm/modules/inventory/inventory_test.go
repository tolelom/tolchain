package inventory

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/vm"
)

func newCtx(t *testing.T, state core.State, fromPubHex string) *vm.Context {
	t.Helper()
	emitter := events.NewEmitter()
	block := &core.Block{Header: core.BlockHeader{
		Height:    1,
		Timestamp: time.Now().UnixNano(),
		PrevHash:  "abc123",
	}}
	tx := &core.Transaction{ID: "tx1", From: fromPubHex, Type: core.TxEquipItem}
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter, EffectiveSender: fromPubHex}
}

// ---------- Equip Tests ----------

func TestEquipItem_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "a1", Slot: "weapon"})

	if err := handleEquipItem(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	inv, _ := state.GetInventory(pubHex)
	if inv.Slots["weapon"] != "a1" {
		t.Errorf("slot weapon = %s, want a1", inv.Slots["weapon"])
	}

	asset, _ := state.GetAsset("a1")
	if equipped, _ := asset.Properties["equipped"].(bool); !equipped {
		t.Error("asset should be marked equipped")
	}
	if slot, _ := asset.Properties["slot"].(string); slot != "weapon" {
		t.Errorf("asset slot = %s, want weapon", slot)
	}
}

func TestEquipItem_EmptyFields(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	// Empty asset_id
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "", Slot: "weapon"})
	if err := handleEquipItem(ctx, payload); err == nil {
		t.Fatal("expected error for empty asset_id")
	}

	// Empty slot
	payload, _ = json.Marshal(core.EquipItemPayload{AssetID: "a1", Slot: ""})
	if err := handleEquipItem(ctx, payload); err == nil {
		t.Fatal("expected error for empty slot")
	}
}

func TestEquipItem_NonOwned(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pub2.Hex(), Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "a1", Slot: "weapon"})

	if err := handleEquipItem(ctx, payload); err == nil {
		t.Fatal("expected error for non-owned asset")
	}
}

func TestEquipItem_Listed(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, ActiveListingID: "l1", Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "a1", Slot: "weapon"})

	if err := handleEquipItem(ctx, payload); err == nil {
		t.Fatal("expected error for listed asset")
	}
}

func TestEquipItem_OccupiedSlot(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a2", Owner: pubHex, Tradeable: true, Properties: map[string]any{}})
	_ = state.SetInventory(&core.Inventory{Owner: pubHex, Slots: map[string]string{"weapon": "a_other"}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "a2", Slot: "weapon"})

	if err := handleEquipItem(ctx, payload); err == nil {
		t.Fatal("expected error for occupied slot")
	}
}

func TestEquipItem_AlreadyEquippedDifferentSlot(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, Properties: map[string]any{"equipped": true, "slot": "head"}})
	_ = state.SetInventory(&core.Inventory{Owner: pubHex, Slots: map[string]string{"head": "a1"}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "a1", Slot: "weapon"})

	if err := handleEquipItem(ctx, payload); err == nil {
		t.Fatal("expected error for already equipped asset in different slot")
	}
}

func TestEquipItem_NonexistentAsset(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "nonexistent", Slot: "weapon"})

	if err := handleEquipItem(ctx, payload); err == nil {
		t.Fatal("expected error for nonexistent asset")
	}
}

// ---------- Unequip Tests ----------

func TestUnequipItem_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, Properties: map[string]any{"equipped": true, "slot": "weapon"}})
	_ = state.SetInventory(&core.Inventory{Owner: pubHex, Slots: map[string]string{"weapon": "a1"}})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxUnequipItem
	payload, _ := json.Marshal(core.UnequipItemPayload{AssetID: "a1"})

	if err := handleUnequipItem(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	inv, _ := state.GetInventory(pubHex)
	if _, ok := inv.Slots["weapon"]; ok {
		t.Error("slot weapon should be freed")
	}

	asset, _ := state.GetAsset("a1")
	if _, ok := asset.Properties["equipped"]; ok {
		t.Error("asset should not have equipped property")
	}
	if _, ok := asset.Properties["slot"]; ok {
		t.Error("asset should not have slot property")
	}
}

func TestUnequipItem_NonOwned(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pub2.Hex(), Tradeable: true, Properties: map[string]any{"equipped": true}})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.UnequipItemPayload{AssetID: "a1"})

	if err := handleUnequipItem(ctx, payload); err == nil {
		t.Fatal("expected error for non-owned unequip")
	}
}

func TestUnequipItem_NotEquipped(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.UnequipItemPayload{AssetID: "a1"})

	if err := handleUnequipItem(ctx, payload); err == nil {
		t.Fatal("expected error for unequipping non-equipped asset")
	}
}

func TestEquipItem_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())

	if err := handleEquipItem(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestUnequipItem_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())

	if err := handleUnequipItem(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestUnequipItem_EmptyAssetID(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.UnequipItemPayload{AssetID: ""})

	if err := handleUnequipItem(ctx, payload); err == nil {
		t.Fatal("expected error for empty asset_id")
	}
}

func TestUnequipItem_AssetNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.UnequipItemPayload{AssetID: "nonexistent"})

	if err := handleUnequipItem(ctx, payload); err == nil {
		t.Fatal("expected error for nonexistent asset")
	}
}

func TestEquipItem_NilProperties(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	// Create asset with nil Properties - the handler should initialise the map.
	_ = state.SetAsset(&core.Asset{ID: "a_nil", Owner: pubHex, Tradeable: true, Properties: nil})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "a_nil", Slot: "weapon"})

	if err := handleEquipItem(ctx, payload); err != nil {
		t.Fatalf("expected success for nil properties, got %v", err)
	}

	asset, _ := state.GetAsset("a_nil")
	if asset.Properties == nil {
		t.Fatal("properties should have been initialised")
	}
	if equipped, _ := asset.Properties["equipped"].(bool); !equipped {
		t.Error("asset should be marked equipped")
	}
	if slot, _ := asset.Properties["slot"].(string); slot != "weapon" {
		t.Errorf("slot = %s, want weapon", slot)
	}
}

func TestEquipItem_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a_ne", Owner: pubHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	payload, _ := json.Marshal(core.EquipItemPayload{AssetID: "a_ne", Slot: "head"})

	if err := handleEquipItem(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestUnequipItem_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a_ue", Owner: pubHex, Tradeable: true, Properties: map[string]any{"equipped": true, "slot": "weapon"}})
	_ = state.SetInventory(&core.Inventory{Owner: pubHex, Slots: map[string]string{"weapon": "a_ue"}})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxUnequipItem
	payload, _ := json.Marshal(core.UnequipItemPayload{AssetID: "a_ue"})

	if err := handleUnequipItem(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}
