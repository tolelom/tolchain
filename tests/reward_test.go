package tests

import (
	"fmt"
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	_ "github.com/tolelom/tolchain/vm/modules/asset"
	_ "github.com/tolelom/tolchain/vm/modules/reward"
)

func TestGrantReward(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)
	// operator has authority
	operator, _ := wallet.Generate()
	exec.SetOperators([]string{operator.PubKey()})

	player, _ := wallet.Generate()
	_ = state.SetAccount(&core.Account{Address: operator.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", operator.PubKey(), nil)

	// Register template
	regTx, _ := operator.NewTx("test-chain", core.TxRegisterTemplate, 0, 0, core.RegisterTemplatePayload{
		ID: "item-tmpl", Name: "Item", Tradeable: true,
	})
	if err := exec.ExecuteTx(block, regTx); err != nil {
		t.Fatal(err)
	}

	// Grant reward: 500 tokens + 2 assets
	rewardTx, _ := operator.GrantReward("test-chain", player.PubKey(), 500, []core.MintAssetPayload{
		{TemplateID: "item-tmpl", Properties: nil},
		{TemplateID: "item-tmpl", Properties: nil},
	}, 1, 0)
	if err := exec.ExecuteTx(block, rewardTx); err != nil {
		t.Fatalf("grant reward: %v", err)
	}

	// Verify tokens
	acc, _ := state.GetAccount(player.PubKey())
	if acc.Balance != 500 {
		t.Errorf("balance: got %d want 500", acc.Balance)
	}

	// Verify assets
	for i := 0; i < 2; i++ {
		assetID := crypto.Hash([]byte(fmt.Sprintf("%s:reward:%d", rewardTx.ID, i)))
		asset, err := state.GetAsset(assetID)
		if err != nil {
			t.Fatalf("asset[%d] not found: %v", i, err)
		}
		if asset.Owner != player.PubKey() {
			t.Errorf("asset[%d] owner mismatch", i)
		}
	}
}

func TestGrantRewardNonOperator(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	operator, _ := wallet.Generate()
	nonOperator, _ := wallet.Generate()
	exec.SetOperators([]string{operator.PubKey()})

	_ = state.SetAccount(&core.Account{Address: nonOperator.PubKey(), Balance: 1000})

	block := core.NewBlock("test-chain", 1, "0000", operator.PubKey(), nil)

	rewardTx, _ := nonOperator.GrantReward("test-chain", operator.PubKey(), 100, nil, 0, 0)
	if err := exec.ExecuteTx(block, rewardTx); err == nil {
		t.Error("non-operator should not be able to grant rewards")
	}
}
