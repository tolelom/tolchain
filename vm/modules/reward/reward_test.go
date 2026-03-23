package reward

import (
	"encoding/json"
	"math"
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
	tx := &core.Transaction{ID: "tx1", From: fromPubHex, Type: core.TxGrantReward}
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter, EffectiveSender: fromPubHex}
}

func TestGrantReward_TokensOnly(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	recipientHex := recipientPub.Hex()

	ctx := newCtx(t, state, pubHex)
	// No operators configured = no restriction.
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   recipientHex,
		TokenAmount: 500,
	})

	if err := handleGrantReward(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	acc, _ := state.GetAccount(recipientHex)
	if acc.Balance != 500 {
		t.Errorf("recipient balance = %d, want 500", acc.Balance)
	}
}

func TestGrantReward_WithAssetMinting(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	recipientHex := recipientPub.Hex()

	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   recipientHex,
		TokenAmount: 100,
		Assets: []core.MintAssetPayload{
			{TemplateID: "t1", Properties: map[string]any{"name": "Reward Sword"}},
		},
	})

	if err := handleGrantReward(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	acc, _ := state.GetAccount(recipientHex)
	if acc.Balance != 100 {
		t.Errorf("recipient balance = %d, want 100", acc.Balance)
	}

	// Check that asset was minted.
	assetID := crypto.Hash([]byte(ctx.Tx.ID + ":reward:0"))
	asset, err := state.GetAsset(assetID)
	if err != nil {
		t.Fatalf("asset not found: %v", err)
	}
	if asset.Owner != recipientHex {
		t.Errorf("asset owner = %s, want %s", asset.Owner, recipientHex)
	}
}

func TestGrantReward_NonOperator(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	_, opPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	ctx := newCtx(t, state, pubHex)
	ctx.Operators = map[string]bool{opPub.Hex(): true} // pubHex is NOT an operator

	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   recipientPub.Hex(),
		TokenAmount: 100,
	})

	if err := handleGrantReward(ctx, payload); err == nil {
		t.Fatal("expected error for non-operator")
	}
}

func TestGrantReward_OperatorSucceeds(t *testing.T) {
	state := testutil.NewStateDB()
	_, opPriv, _ := crypto.GenerateKeyPair()
	_ = opPriv
	_, opPub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	opHex := opPub.Hex()

	ctx := newCtx(t, state, opHex)
	ctx.Operators = map[string]bool{opHex: true}

	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   recipientPub.Hex(),
		TokenAmount: 200,
	})

	if err := handleGrantReward(ctx, payload); err != nil {
		t.Fatalf("expected success for operator, got %v", err)
	}
}

func TestGrantReward_EmptyRecipient(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   "",
		TokenAmount: 100,
	})

	if err := handleGrantReward(ctx, payload); err == nil {
		t.Fatal("expected error for empty recipient")
	}
}

func TestGrantReward_InvalidRecipientPubkey(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   "not-a-valid-key",
		TokenAmount: 100,
	})

	if err := handleGrantReward(ctx, payload); err == nil {
		t.Fatal("expected error for invalid recipient pubkey")
	}
}

func TestGrantReward_BalanceOverflow(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	recipientHex := recipientPub.Hex()

	_ = state.SetAccount(&core.Account{Address: recipientHex, Balance: math.MaxUint64 - 50})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   recipientHex,
		TokenAmount: 100,
	})

	if err := handleGrantReward(ctx, payload); err == nil {
		t.Fatal("expected error for balance overflow")
	}
}

func TestGrantReward_TemplateNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient: recipientPub.Hex(),
		Assets: []core.MintAssetPayload{
			{TemplateID: "nonexistent"},
		},
	})

	if err := handleGrantReward(ctx, payload); err == nil {
		t.Fatal("expected error for template not found")
	}
}

func TestGrantReward_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	if err := handleGrantReward(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestGrantReward_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	ctx.Emitter = nil
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   recipientPub.Hex(),
		TokenAmount: 100,
	})

	if err := handleGrantReward(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestGrantReward_ZeroTokenWithAssets(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	recipientHex := recipientPub.Hex()

	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Shield", Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient:   recipientHex,
		TokenAmount: 0, // no tokens, just assets
		Assets: []core.MintAssetPayload{
			{TemplateID: "t1", Properties: map[string]any{"name": "Reward Shield"}},
		},
	})

	if err := handleGrantReward(ctx, payload); err != nil {
		t.Fatalf("expected success for zero-token with assets: %v", err)
	}

	// Verify token balance is still 0.
	acc, _ := state.GetAccount(recipientHex)
	if acc.Balance != 0 {
		t.Errorf("balance = %d, want 0", acc.Balance)
	}

	// Verify asset was minted.
	assetID := crypto.Hash([]byte(ctx.Tx.ID + ":reward:0"))
	asset, err := state.GetAsset(assetID)
	if err != nil {
		t.Fatalf("asset not found: %v", err)
	}
	if asset.Owner != recipientHex {
		t.Errorf("asset owner = %s, want %s", asset.Owner, recipientHex)
	}
}

func TestGrantReward_NilEmitterWithAssets(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, recipientPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	payload, _ := json.Marshal(core.GrantRewardPayload{
		Recipient: recipientPub.Hex(),
		Assets: []core.MintAssetPayload{
			{TemplateID: "t1", Properties: map[string]any{"name": "Nil Emitter Sword"}},
		},
	})

	if err := handleGrantReward(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter and assets: %v", err)
	}
}
