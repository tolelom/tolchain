package asset

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
	tx := &core.Transaction{ID: "tx1", From: fromPubHex, Type: core.TxMintAsset}
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter, EffectiveSender: fromPubHex}
}

// ---------- Mint Tests ----------

func TestMintAsset_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Schema: nil, Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.MintAssetPayload{TemplateID: "t1"})

	if err := handleMintAsset(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	// Verify asset was created with deterministic ID.
	assetID := crypto.Hash([]byte(ctx.Tx.ID + ":asset:" + "t1"))
	asset, err := state.GetAsset(assetID)
	if err != nil {
		t.Fatalf("asset not found: %v", err)
	}
	if asset.Owner != pubHex {
		t.Errorf("owner = %s, want %s", asset.Owner, pubHex)
	}
	if asset.TemplateID != "t1" {
		t.Errorf("template_id = %s, want t1", asset.TemplateID)
	}
}

func TestMintAsset_EmptyTemplateID(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.MintAssetPayload{TemplateID: ""})

	if err := handleMintAsset(ctx, payload); err == nil {
		t.Fatal("expected error for empty template_id")
	}
}

func TestMintAsset_NonexistentTemplate(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.MintAssetPayload{TemplateID: "nonexistent"})

	if err := handleMintAsset(ctx, payload); err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}

func TestMintAsset_InvalidOwnerPubkey(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.MintAssetPayload{TemplateID: "t1", Owner: "bad-pubkey"})

	if err := handleMintAsset(ctx, payload); err == nil {
		t.Fatal("expected error for invalid owner pubkey")
	}
}

func TestMintAsset_SchemaValidation_StringViolation(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	schema := map[string]any{"name": "string"}
	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Schema: schema, Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	// Provide a non-string value for "name".
	payload, _ := json.Marshal(core.MintAssetPayload{
		TemplateID: "t1",
		Properties: map[string]any{"name": 123},
	})

	if err := handleMintAsset(ctx, payload); err == nil {
		t.Fatal("expected schema validation error")
	}
}

func TestMintAsset_SchemaValidation_ExtraProperty(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	schema := map[string]any{"name": "string"}
	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Schema: schema, Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.MintAssetPayload{
		TemplateID: "t1",
		Properties: map[string]any{"name": "Excalibur", "extra": "bad"},
	})

	if err := handleMintAsset(ctx, payload); err == nil {
		t.Fatal("expected error for extra property not in schema")
	}
}

func TestMintAsset_SchemaValidation_IntValid(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	schema := map[string]any{"damage": "int"}
	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Schema: schema, Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	// JSON numbers decode as float64; 42.0 is a valid integer.
	payload, _ := json.Marshal(core.MintAssetPayload{
		TemplateID: "t1",
		Properties: map[string]any{"damage": float64(42)},
	})

	if err := handleMintAsset(ctx, payload); err != nil {
		t.Fatalf("expected success for valid int property, got %v", err)
	}
}

func TestMintAsset_SchemaValidation_IntInvalid(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	schema := map[string]any{"damage": "int"}
	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Schema: schema, Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	// 3.14 is not an integer.
	payload, _ := json.Marshal(core.MintAssetPayload{
		TemplateID: "t1",
		Properties: map[string]any{"damage": 3.14},
	})

	if err := handleMintAsset(ctx, payload); err == nil {
		t.Fatal("expected error for non-integer float")
	}
}

func TestMintAsset_SchemaValidation_BoolValid(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	schema := map[string]any{"rare": "bool"}
	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Schema: schema, Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.MintAssetPayload{
		TemplateID: "t1",
		Properties: map[string]any{"rare": true},
	})

	if err := handleMintAsset(ctx, payload); err != nil {
		t.Fatalf("expected success for bool property, got %v", err)
	}
}

func TestMintAsset_SchemaValidation_BoolInvalid(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	schema := map[string]any{"rare": "bool"}
	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Schema: schema, Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.MintAssetPayload{
		TemplateID: "t1",
		Properties: map[string]any{"rare": "yes"},
	})

	if err := handleMintAsset(ctx, payload); err == nil {
		t.Fatal("expected error for non-bool value")
	}
}

// ---------- Burn Tests ----------

func TestBurnAsset_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxBurnAsset
	payload, _ := json.Marshal(core.BurnAssetPayload{AssetID: "a1"})

	if err := handleBurnAsset(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	if _, err := state.GetAsset("a1"); err == nil {
		t.Fatal("expected asset to be deleted")
	}
}

func TestBurnAsset_NonOwner(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pub2.Hex(), Properties: map[string]any{}})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.BurnAssetPayload{AssetID: "a1"})

	if err := handleBurnAsset(ctx, payload); err == nil {
		t.Fatal("expected error for non-owner burn")
	}
}

func TestBurnAsset_Nonexistent(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.BurnAssetPayload{AssetID: "nonexistent"})

	if err := handleBurnAsset(ctx, payload); err == nil {
		t.Fatal("expected error for nonexistent asset")
	}
}

func TestBurnAsset_ActiveListing(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, ActiveListingID: "listing1", Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.BurnAssetPayload{AssetID: "a1"})

	if err := handleBurnAsset(ctx, payload); err == nil {
		t.Fatal("expected error for asset with active listing")
	}
}

func TestBurnAsset_Equipped(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Properties: map[string]any{"equipped": true}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.BurnAssetPayload{AssetID: "a1"})

	if err := handleBurnAsset(ctx, payload); err == nil {
		t.Fatal("expected error for equipped asset")
	}
}

// ---------- Transfer Asset Tests ----------

func TestTransferAsset_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()
	recipientHex := pub2.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: senderHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, senderHex)
	ctx.Tx.Type = core.TxTransferAsset
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a1", To: recipientHex})

	if err := handleTransferAsset(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	asset, _ := state.GetAsset("a1")
	if asset.Owner != recipientHex {
		t.Errorf("owner = %s, want %s", asset.Owner, recipientHex)
	}
}

func TestTransferAsset_NonOwner(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	_, pub3, _ := crypto.GenerateKeyPair()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pub2.Hex(), Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a1", To: pub3.Hex()})

	if err := handleTransferAsset(ctx, payload); err == nil {
		t.Fatal("expected error for non-owner transfer")
	}
}

func TestTransferAsset_NotTradeable(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: senderHex, Tradeable: false, Properties: map[string]any{}})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a1", To: pub2.Hex()})

	if err := handleTransferAsset(ctx, payload); err == nil {
		t.Fatal("expected error for non-tradeable asset")
	}
}

func TestTransferAsset_Listed(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: senderHex, Tradeable: true, ActiveListingID: "l1", Properties: map[string]any{}})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a1", To: pub2.Hex()})

	if err := handleTransferAsset(ctx, payload); err == nil {
		t.Fatal("expected error for listed asset")
	}
}

func TestTransferAsset_Equipped(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: senderHex, Tradeable: true, Properties: map[string]any{"equipped": true}})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a1", To: pub2.Hex()})

	if err := handleTransferAsset(ctx, payload); err == nil {
		t.Fatal("expected error for equipped asset")
	}
}

func TestTransferAsset_InvalidToPubkey(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: senderHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, senderHex)
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a1", To: "bad-pubkey"})

	if err := handleTransferAsset(ctx, payload); err == nil {
		t.Fatal("expected error for invalid to pubkey")
	}
}

// ---------- Register Template Tests ----------

func TestRegisterTemplate_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxRegisterTemplate
	payload, _ := json.Marshal(core.RegisterTemplatePayload{ID: "t1", Name: "Sword", Tradeable: true})

	if err := handleRegisterTemplate(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	tmpl, err := state.GetTemplate("t1")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "Sword" {
		t.Errorf("name = %s, want Sword", tmpl.Name)
	}
	if tmpl.Creator != pubHex {
		t.Errorf("creator = %s, want %s", tmpl.Creator, pubHex)
	}
}

func TestRegisterTemplate_EmptyID(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.RegisterTemplatePayload{ID: "", Name: "Sword"})

	if err := handleRegisterTemplate(ctx, payload); err == nil {
		t.Fatal("expected error for empty template id")
	}
}

func TestRegisterTemplate_Duplicate(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Existing", Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.RegisterTemplatePayload{ID: "t1", Name: "Duplicate"})

	if err := handleRegisterTemplate(ctx, payload); err == nil {
		t.Fatal("expected error for duplicate template")
	}
}

func TestRegisterTemplate_OperatorRestriction(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, opPub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	ctx.Operators = map[string]bool{opPub.Hex(): true} // pub is NOT an operator
	payload, _ := json.Marshal(core.RegisterTemplatePayload{ID: "t1", Name: "Sword"})

	if err := handleRegisterTemplate(ctx, payload); err == nil {
		t.Fatal("expected error for non-operator")
	}
}

func TestBurnAsset_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	if err := handleBurnAsset(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestTransferAsset_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	if err := handleTransferAsset(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestTransferAsset_EmptyTo(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, Properties: map[string]any{}})
	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a1", To: ""})
	if err := handleTransferAsset(ctx, payload); err == nil {
		t.Fatal("expected error for empty To")
	}
}

func TestMintAsset_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	if err := handleMintAsset(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestRegisterTemplate_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	ctx := newCtx(t, state, pub.Hex())
	if err := handleRegisterTemplate(ctx, json.RawMessage(`{invalid`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateSchema_UnknownTypeHint(t *testing.T) {
	// An unknown schema type hint should be silently skipped (no error).
	schema := map[string]any{"custom_field": "unknown_type"}
	props := map[string]any{"custom_field": "any_value"}

	if err := ValidateSchema(schema, props); err != nil {
		t.Fatalf("expected success for unknown type hint, got %v", err)
	}
}

func TestValidateSchema_IntWithJsonNumber(t *testing.T) {
	// json.Number is an alternative representation when using Decoder.UseNumber().
	schema := map[string]any{"level": "int"}
	props := map[string]any{"level": json.Number("42")}

	if err := ValidateSchema(schema, props); err != nil {
		t.Fatalf("expected success for json.Number int, got %v", err)
	}
}

func TestValidateSchema_IntWithInvalidJsonNumber(t *testing.T) {
	// A json.Number that is not a valid integer should fail.
	schema := map[string]any{"level": "int"}
	props := map[string]any{"level": json.Number("3.14")}

	if err := ValidateSchema(schema, props); err == nil {
		t.Fatal("expected error for non-integer json.Number")
	}
}

func TestValidateSchema_IntWithWrongType(t *testing.T) {
	// Passing a string where int is expected (not float64 or json.Number).
	schema := map[string]any{"damage": "int"}
	props := map[string]any{"damage": "not_a_number"}

	if err := ValidateSchema(schema, props); err == nil {
		t.Fatal("expected error for string where int expected")
	}
}

func TestValidateSchema_NonStringTypeHint(t *testing.T) {
	// Schema type hint is not a string (e.g., an int) - should fall to default/skip.
	schema := map[string]any{"weird": 42}
	props := map[string]any{"weird": "anything"}

	if err := ValidateSchema(schema, props); err != nil {
		t.Fatalf("expected success for non-string type hint, got %v", err)
	}
}

func TestMintAsset_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	payload, _ := json.Marshal(core.MintAssetPayload{TemplateID: "t1"})

	if err := handleMintAsset(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestBurnAsset_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a_ne", Owner: pubHex, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxBurnAsset
	payload, _ := json.Marshal(core.BurnAssetPayload{AssetID: "a_ne"})

	if err := handleBurnAsset(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestTransferAsset_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()
	senderHex := pub.Hex()
	recipientHex := pub2.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a_ne2", Owner: senderHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, senderHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxTransferAsset
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "a_ne2", To: recipientHex})

	if err := handleTransferAsset(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestRegisterTemplate_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxRegisterTemplate
	payload, _ := json.Marshal(core.RegisterTemplatePayload{ID: "t_ne", Name: "Shield", Tradeable: true})

	if err := handleRegisterTemplate(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestTransferAsset_Nonexistent(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.TransferAssetPayload{AssetID: "nonexistent", To: pub2.Hex()})

	if err := handleTransferAsset(ctx, payload); err == nil {
		t.Fatal("expected error for nonexistent asset")
	}
}

func TestMintAsset_WithExplicitOwner(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, ownerPub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	ownerHex := ownerPub.Hex()

	_ = state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Tradeable: true, Creator: pubHex})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.MintAssetPayload{TemplateID: "t1", Owner: ownerHex})

	if err := handleMintAsset(ctx, payload); err != nil {
		t.Fatalf("expected success with explicit owner, got %v", err)
	}

	assetID := crypto.Hash([]byte(ctx.Tx.ID + ":asset:" + "t1"))
	asset, _ := state.GetAsset(assetID)
	if asset.Owner != ownerHex {
		t.Errorf("owner = %s, want %s", asset.Owner, ownerHex)
	}
}

func TestValidateSchema_MissingOptionalProperty(t *testing.T) {
	// Schema has a field but the properties map doesn't include it. Should succeed.
	schema := map[string]any{"name": "string", "rarity": "int"}
	props := map[string]any{"name": "Excalibur"} // rarity is missing but optional

	if err := ValidateSchema(schema, props); err != nil {
		t.Fatalf("expected success for missing optional property, got %v", err)
	}
}
