package tests

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	// Register VM modules
	_ "github.com/tolelom/tolchain/vm/modules/asset"
	_ "github.com/tolelom/tolchain/vm/modules/economy"
	_ "github.com/tolelom/tolchain/vm/modules/market"
	_ "github.com/tolelom/tolchain/vm/modules/session"
)

func newInMemState(t *testing.T) core.State {
	t.Helper()
	return storage.NewStateDB(testutil.NewMemDB())
}

// TestTokenTransfer verifies that the economy transfer handler moves tokens.
func TestTokenTransfer(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	sender, _ := wallet.Generate()
	receiver, _ := wallet.Generate()

	_ = state.SetAccount(&core.Account{Address: sender.PubKey(), Balance: 1000})

	tx, err := sender.Transfer(receiver.PubKey(), 300, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	block := core.NewBlock(1, "0000", sender.PubKey(), []*core.Transaction{tx})
	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("ExecuteTx: %v", err)
	}

	senderAcc, _ := state.GetAccount(sender.PubKey())
	if senderAcc.Balance != 700 {
		t.Errorf("sender balance: got %d want 700", senderAcc.Balance)
	}
	receiverAcc, _ := state.GetAccount(receiver.PubKey())
	if receiverAcc.Balance != 300 {
		t.Errorf("receiver balance: got %d want 300", receiverAcc.Balance)
	}
}

// TestMintAsset verifies that an asset is stored with correct fields after minting.
func TestMintAsset(t *testing.T) {
	state := newInMemState(t)
	emitter := events.NewEmitter()
	exec := vm.NewExecutor(state, emitter)

	creator, _ := wallet.Generate()
	_ = state.SetAccount(&core.Account{Address: creator.PubKey(), Balance: 1000})

	block := core.NewBlock(1, "0000", creator.PubKey(), nil)

	// Register template (nonce=0)
	regTx, err := creator.NewTx(core.TxRegisterTemplate, 0, 0, core.RegisterTemplatePayload{
		ID:        "sword-template",
		Name:      "Sword",
		Tradeable: true,
		Schema:    map[string]any{"attack": "int"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.ExecuteTx(block, regTx); err != nil {
		t.Fatalf("register template: %v", err)
	}

	// Mint asset (nonce=1)
	mintTx, err := creator.NewTx(core.TxMintAsset, 1, 0, core.MintAssetPayload{
		TemplateID: "sword-template",
		Owner:      creator.PubKey(),
		Properties: map[string]any{"attack": 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.ExecuteTx(block, mintTx); err != nil {
		t.Fatalf("mint asset: %v", err)
	}

	// Compute the expected deterministic asset ID (mirrors asset handler logic)
	expectedID := crypto.Hash([]byte(mintTx.ID + ":asset:sword-template"))

	asset, err := state.GetAsset(expectedID)
	if err != nil {
		t.Fatalf("GetAsset(%s): %v", expectedID, err)
	}
	if asset.Owner != creator.PubKey() {
		t.Errorf("owner: got %s want %s", asset.Owner, creator.PubKey())
	}
	if asset.TemplateID != "sword-template" {
		t.Errorf("template_id: got %s want sword-template", asset.TemplateID)
	}
	if !asset.Tradeable {
		t.Error("asset should be tradeable (inherited from template)")
	}
}

// TestNonceReplay verifies that replaying a transaction with the same nonce fails.
func TestNonceReplay(t *testing.T) {
	state := newInMemState(t)
	exec := vm.NewExecutor(state, events.NewEmitter())

	w, _ := wallet.Generate()
	_ = state.SetAccount(&core.Account{Address: w.PubKey(), Balance: 1000})

	block := core.NewBlock(1, "0000", w.PubKey(), nil)

	tx1, _ := w.Transfer("aabb", 1, 0, 0)
	if err := exec.ExecuteTx(block, tx1); err != nil {
		t.Fatalf("first tx: %v", err)
	}
	// Replay (same nonce=0, already consumed)
	if err := exec.ExecuteTx(block, tx1); err == nil {
		t.Error("replay should fail due to nonce mismatch")
	}
}
