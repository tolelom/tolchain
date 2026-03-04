package tests

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	_ "github.com/tolelom/tolchain/vm/modules/asset"
	_ "github.com/tolelom/tolchain/vm/modules/inventory"
)

func TestEquipUnequip(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	w, _ := wallet.Generate()
	_ = state.SetAccount(&core.Account{Address: w.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", w.PubKey(), nil)

	// Register template + mint asset
	regTx, _ := w.NewTx("test-chain", core.TxRegisterTemplate, 0, 0, core.RegisterTemplatePayload{
		ID: "sword-tmpl", Name: "Sword", Tradeable: true, Schema: map[string]any{"attack": "int"},
	})
	if err := exec.ExecuteTx(block, regTx); err != nil {
		t.Fatal(err)
	}

	mintTx, _ := w.MintAsset("test-chain", "sword-tmpl", w.PubKey(), map[string]any{"attack": 50}, 1, 0)
	if err := exec.ExecuteTx(block, mintTx); err != nil {
		t.Fatal(err)
	}
	assetID := crypto.Hash([]byte(mintTx.ID + ":asset:sword-tmpl"))

	// Equip
	equipTx, _ := w.EquipItem("test-chain", assetID, "weapon", 2, 0)
	if err := exec.ExecuteTx(block, equipTx); err != nil {
		t.Fatalf("equip: %v", err)
	}

	// Verify inventory
	inv, _ := state.GetInventory(w.PubKey())
	if inv.Slots["weapon"] != assetID {
		t.Errorf("slot weapon: got %s want %s", inv.Slots["weapon"], assetID)
	}

	// Verify asset properties
	asset, _ := state.GetAsset(assetID)
	if equipped, _ := asset.Properties["equipped"].(bool); !equipped {
		t.Error("asset should be equipped")
	}

	// Unequip
	unequipTx, _ := w.UnequipItem("test-chain", assetID, 3, 0)
	if err := exec.ExecuteTx(block, unequipTx); err != nil {
		t.Fatalf("unequip: %v", err)
	}

	inv, _ = state.GetInventory(w.PubKey())
	if _, ok := inv.Slots["weapon"]; ok {
		t.Error("weapon slot should be empty after unequip")
	}
	asset, _ = state.GetAsset(assetID)
	if _, ok := asset.Properties["equipped"]; ok {
		t.Error("equipped property should be removed")
	}
}

func TestEquipSlotConflict(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	w, _ := wallet.Generate()
	_ = state.SetAccount(&core.Account{Address: w.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", w.PubKey(), nil)

	regTx, _ := w.NewTx("test-chain", core.TxRegisterTemplate, 0, 0, core.RegisterTemplatePayload{
		ID: "sword-tmpl", Name: "Sword", Tradeable: true,
	})
	_ = exec.ExecuteTx(block, regTx)

	mint1, _ := w.MintAsset("test-chain", "sword-tmpl", w.PubKey(), nil, 1, 0)
	_ = exec.ExecuteTx(block, mint1)
	asset1ID := crypto.Hash([]byte(mint1.ID + ":asset:sword-tmpl"))

	mint2, _ := w.MintAsset("test-chain", "sword-tmpl", w.PubKey(), nil, 2, 0)
	_ = exec.ExecuteTx(block, mint2)
	asset2ID := crypto.Hash([]byte(mint2.ID + ":asset:sword-tmpl"))

	// Equip first asset to weapon slot
	equip1, _ := w.EquipItem("test-chain", asset1ID, "weapon", 3, 0)
	if err := exec.ExecuteTx(block, equip1); err != nil {
		t.Fatal(err)
	}

	// Try to equip second asset to same slot — should fail
	equip2, _ := w.EquipItem("test-chain", asset2ID, "weapon", 4, 0)
	if err := exec.ExecuteTx(block, equip2); err == nil {
		t.Error("should fail: slot already occupied")
	}
}

func TestEquippedAssetCannotTransfer(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	w, _ := wallet.Generate()
	other, _ := wallet.Generate()
	_ = state.SetAccount(&core.Account{Address: w.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", w.PubKey(), nil)

	regTx, _ := w.NewTx("test-chain", core.TxRegisterTemplate, 0, 0, core.RegisterTemplatePayload{
		ID: "sword-tmpl", Name: "Sword", Tradeable: true,
	})
	_ = exec.ExecuteTx(block, regTx)

	mintTx, _ := w.MintAsset("test-chain", "sword-tmpl", w.PubKey(), nil, 1, 0)
	_ = exec.ExecuteTx(block, mintTx)
	assetID := crypto.Hash([]byte(mintTx.ID + ":asset:sword-tmpl"))

	// Equip
	equipTx, _ := w.EquipItem("test-chain", assetID, "weapon", 2, 0)
	_ = exec.ExecuteTx(block, equipTx)

	// Try transfer — should fail
	transferTx, _ := w.TransferAsset("test-chain", assetID, other.PubKey(), 3, 0)
	if err := exec.ExecuteTx(block, transferTx); err == nil {
		t.Error("should fail: asset is equipped")
	}

	// Try list on market — should fail
	listTx, _ := w.ListMarket("test-chain", assetID, 100, 3, 0)
	if err := exec.ExecuteTx(block, listTx); err == nil {
		t.Error("should fail: asset is equipped")
	}
}
