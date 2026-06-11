package market

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
	tx := &core.Transaction{ID: "tx1", From: fromPubHex, Type: core.TxListMarket}
	return &vm.Context{State: state, Block: block, Tx: tx, Emitter: emitter, EffectiveSender: fromPubHex}
}

// ---------- List Market Tests ----------

func TestListMarket_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "a1", Price: 500})

	if err := handleListMarket(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	// Verify listing was created.
	listingID := crypto.Hash([]byte(ctx.Tx.ID + ":listing:" + "a1"))
	listing, err := state.GetListing(listingID)
	if err != nil {
		t.Fatalf("listing not found: %v", err)
	}
	if !listing.Active {
		t.Error("listing should be active")
	}
	if listing.Price != 500 {
		t.Errorf("price = %d, want 500", listing.Price)
	}

	// Verify asset has active listing ID set.
	asset, _ := state.GetAsset("a1")
	if asset.ActiveListingID != listingID {
		t.Errorf("active_listing_id = %s, want %s", asset.ActiveListingID, listingID)
	}
}

func TestListMarket_ZeroPrice(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "a1", Price: 0})

	if err := handleListMarket(ctx, payload); err == nil {
		t.Fatal("expected error for zero price")
	}
}

func TestListMarket_NonOwner(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pub2.Hex(), Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "a1", Price: 100})

	if err := handleListMarket(ctx, payload); err == nil {
		t.Fatal("expected error for non-owner listing")
	}
}

func TestListMarket_NotTradeable(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: false, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "a1", Price: 100})

	if err := handleListMarket(ctx, payload); err == nil {
		t.Fatal("expected error for non-tradeable asset")
	}
}

func TestListMarket_Equipped(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, Properties: map[string]any{"equipped": true}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "a1", Price: 100})

	if err := handleListMarket(ctx, payload); err == nil {
		t.Fatal("expected error for equipped asset")
	}
}

func TestListMarket_DoubleListing(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, ActiveListingID: "existing", Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "a1", Price: 100})

	if err := handleListMarket(ctx, payload); err == nil {
		t.Fatal("expected error for double listing")
	}
}

// ---------- Buy Market Tests ----------

func TestBuyMarket_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, sellerPub, _ := crypto.GenerateKeyPair()
	_, buyerPub, _ := crypto.GenerateKeyPair()
	sellerHex := sellerPub.Hex()
	buyerHex := buyerPub.Hex()

	// Set up asset, listing, and balances.
	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: sellerHex, Tradeable: true, ActiveListingID: "l1", Properties: map[string]any{}})
	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: sellerHex, Price: 200, Active: true})
	_ = state.SetAccount(&core.Account{Address: buyerHex, Balance: 500})
	_ = state.SetAccount(&core.Account{Address: sellerHex, Balance: 100})

	ctx := newCtx(t, state, buyerHex)
	ctx.Tx.Type = core.TxBuyMarket
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	buyer, _ := state.GetAccount(buyerHex)
	seller, _ := state.GetAccount(sellerHex)
	if buyer.Balance != 300 {
		t.Errorf("buyer balance = %d, want 300", buyer.Balance)
	}
	if seller.Balance != 300 {
		t.Errorf("seller balance = %d, want 300", seller.Balance)
	}

	asset, _ := state.GetAsset("a1")
	if asset.Owner != buyerHex {
		t.Errorf("asset owner = %s, want %s", asset.Owner, buyerHex)
	}
	if asset.ActiveListingID != "" {
		t.Errorf("active_listing_id = %s, want empty", asset.ActiveListingID)
	}

	listing, _ := state.GetListing("l1")
	if listing.Active {
		t.Error("listing should be inactive after buy")
	}
}

func TestBuyMarket_InactiveListing(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: "seller", Price: 100, Active: false})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err == nil {
		t.Fatal("expected error for inactive listing")
	}
}

func TestBuyMarket_OwnListing(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: pubHex, Price: 100, Active: true})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err == nil {
		t.Fatal("expected error for buying own listing")
	}
}

func TestBuyMarket_InsufficientBalance(t *testing.T) {
	state := testutil.NewStateDB()
	_, sellerPub, _ := crypto.GenerateKeyPair()
	_, buyerPub, _ := crypto.GenerateKeyPair()

	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: sellerPub.Hex(), Price: 500, Active: true})
	_ = state.SetAccount(&core.Account{Address: buyerPub.Hex(), Balance: 100})

	ctx := newCtx(t, state, buyerPub.Hex())
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err == nil {
		t.Fatal("expected error for insufficient balance")
	}
}

// TestBuyMarket_DelegatedExceedsMaxAmount is a regression test: a delegated
// buy_market must enforce the delegation grant's MaxAmount limit, the same way
// transfer and grant_reward do via vm.ChargeDelegationAmount.
func TestBuyMarket_DelegatedExceedsMaxAmount(t *testing.T) {
	state := testutil.NewStateDB()
	_, sellerPub, _ := crypto.GenerateKeyPair()
	_, granterPub, _ := crypto.GenerateKeyPair() // buyer (on whose behalf)
	_, granteePub, _ := crypto.GenerateKeyPair() // delegated sender
	sellerHex := sellerPub.Hex()
	granterHex := granterPub.Hex()
	granteeHex := granteePub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: sellerHex, Tradeable: true, ActiveListingID: "l1", Properties: map[string]any{}})
	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: sellerHex, Price: 200, Active: true})
	_ = state.SetAccount(&core.Account{Address: granterHex, Balance: 500})
	_ = state.SetDelegation(&core.DelegationGrant{
		Granter:    granterHex,
		Grantee:    granteeHex,
		AllowTypes: []string{string(core.TxBuyMarket)},
		MaxAmount:  50, // below the listing price
	})

	ctx := newCtx(t, state, granterHex)
	ctx.Tx = &core.Transaction{ID: "tx1", From: granteeHex, OnBehalfOf: granterHex, Type: core.TxBuyMarket}
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err == nil {
		t.Fatal("expected error: delegated buy exceeds delegation MaxAmount")
	}
}

func TestBuyMarket_DelegatedChargesSpentAmount(t *testing.T) {
	state := testutil.NewStateDB()
	_, sellerPub, _ := crypto.GenerateKeyPair()
	_, granterPub, _ := crypto.GenerateKeyPair()
	_, granteePub, _ := crypto.GenerateKeyPair()
	sellerHex := sellerPub.Hex()
	granterHex := granterPub.Hex()
	granteeHex := granteePub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: sellerHex, Tradeable: true, ActiveListingID: "l1", Properties: map[string]any{}})
	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: sellerHex, Price: 200, Active: true})
	_ = state.SetAccount(&core.Account{Address: granterHex, Balance: 500})
	_ = state.SetDelegation(&core.DelegationGrant{
		Granter:    granterHex,
		Grantee:    granteeHex,
		AllowTypes: []string{string(core.TxBuyMarket)},
		MaxAmount:  500,
	})

	ctx := newCtx(t, state, granterHex)
	ctx.Tx = &core.Transaction{ID: "tx1", From: granteeHex, OnBehalfOf: granterHex, Type: core.TxBuyMarket}
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err != nil {
		t.Fatalf("delegated buy within MaxAmount should succeed: %v", err)
	}

	grant, err := state.GetDelegation(granterHex, granteeHex)
	if err != nil {
		t.Fatal(err)
	}
	if grant.SpentAmount != 200 {
		t.Errorf("grant.SpentAmount = %d, want 200", grant.SpentAmount)
	}
}

// ---------- Cancel Listing Tests ----------

func TestCancelListing_Success(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: pubHex, Tradeable: true, ActiveListingID: "l1", Properties: map[string]any{}})
	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: pubHex, Price: 100, Active: true})

	ctx := newCtx(t, state, pubHex)
	ctx.Tx.Type = core.TxCancelListing
	payload, _ := json.Marshal(core.CancelListingPayload{ListingID: "l1"})

	if err := handleCancelListing(ctx, payload); err != nil {
		t.Fatalf("expected success, got %v", err)
	}

	listing, _ := state.GetListing("l1")
	if listing.Active {
		t.Error("listing should be inactive after cancel")
	}

	asset, _ := state.GetAsset("a1")
	if asset.ActiveListingID != "" {
		t.Errorf("active_listing_id = %s, want empty", asset.ActiveListingID)
	}
}

func TestCancelListing_NonSeller(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	_, pub2, _ := crypto.GenerateKeyPair()

	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: pub2.Hex(), Price: 100, Active: true})

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.CancelListingPayload{ListingID: "l1"})

	if err := handleCancelListing(ctx, payload); err == nil {
		t.Fatal("expected error for non-seller cancel")
	}
}

func TestCancelListing_Inactive(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: pubHex, Price: 100, Active: false})

	ctx := newCtx(t, state, pubHex)
	payload, _ := json.Marshal(core.CancelListingPayload{ListingID: "l1"})

	if err := handleCancelListing(ctx, payload); err == nil {
		t.Fatal("expected error for cancelling inactive listing")
	}
}

func TestBuyMarket_SellerBalanceOverflow(t *testing.T) {
	state := testutil.NewStateDB()
	_, sellerPub, _ := crypto.GenerateKeyPair()
	_, buyerPub, _ := crypto.GenerateKeyPair()
	sellerHex := sellerPub.Hex()
	buyerHex := buyerPub.Hex()

	// Set seller balance near max so adding price causes overflow
	_ = state.SetAccount(&core.Account{Address: sellerHex, Balance: math.MaxUint64 - 10})
	_ = state.SetAccount(&core.Account{Address: buyerHex, Balance: 1000})

	_ = state.SetAsset(&core.Asset{ID: "a1", Owner: sellerHex, Tradeable: true, ActiveListingID: "l1", Properties: map[string]any{}})
	_ = state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: sellerHex, Price: 100, Active: true})

	ctx := newCtx(t, state, buyerHex)
	ctx.Tx.Type = core.TxBuyMarket
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err == nil {
		t.Fatal("expected error for seller balance overflow")
	}
}

func TestBuyMarket_ListingNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "nonexistent"})

	if err := handleBuyMarket(ctx, payload); err == nil {
		t.Fatal("expected error for listing not found")
	}
}

func TestBuyMarket_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	if err := handleBuyMarket(ctx, []byte("invalid json")); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestListMarket_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	if err := handleListMarket(ctx, []byte("invalid json")); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestListMarket_AssetNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "nonexistent", Price: 100})

	if err := handleListMarket(ctx, payload); err == nil {
		t.Fatal("expected error for asset not found")
	}
}

func TestCancelListing_InvalidPayload(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())

	if err := handleCancelListing(ctx, []byte("invalid json")); err == nil {
		t.Fatal("expected error for invalid payload")
	}
}

func TestCancelListing_ListingNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()

	ctx := newCtx(t, state, pub.Hex())
	payload, _ := json.Marshal(core.CancelListingPayload{ListingID: "nonexistent"})

	if err := handleCancelListing(ctx, payload); err == nil {
		t.Fatal("expected error for listing not found")
	}
}

func TestBuyMarket_AssetNotFound(t *testing.T) {
	state := testutil.NewStateDB()
	_, sellerPub, _ := crypto.GenerateKeyPair()
	_, buyerPub, _ := crypto.GenerateKeyPair()
	sellerHex := sellerPub.Hex()
	buyerHex := buyerPub.Hex()

	state.SetAccount(&core.Account{Address: buyerHex, Balance: 1000})
	state.SetAccount(&core.Account{Address: sellerHex, Balance: 100})
	// Listing references nonexistent asset
	state.SetListing(&core.MarketListing{ID: "l1", AssetID: "nonexistent", Seller: sellerHex, Price: 50, Active: true})

	ctx := newCtx(t, state, buyerHex)
	ctx.Tx.Type = core.TxBuyMarket
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1"})

	if err := handleBuyMarket(ctx, payload); err == nil {
		t.Fatal("expected error for nonexistent asset")
	}
}

func TestListMarket_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1_ne", Owner: pubHex, Tradeable: true, Properties: map[string]any{}})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	payload, _ := json.Marshal(core.ListMarketPayload{AssetID: "a1_ne", Price: 500})

	if err := handleListMarket(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestBuyMarket_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, sellerPub, _ := crypto.GenerateKeyPair()
	_, buyerPub, _ := crypto.GenerateKeyPair()
	sellerHex := sellerPub.Hex()
	buyerHex := buyerPub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1_ne", Owner: sellerHex, Tradeable: true, ActiveListingID: "l1_ne", Properties: map[string]any{}})
	_ = state.SetListing(&core.MarketListing{ID: "l1_ne", AssetID: "a1_ne", Seller: sellerHex, Price: 200, Active: true})
	_ = state.SetAccount(&core.Account{Address: buyerHex, Balance: 500})
	_ = state.SetAccount(&core.Account{Address: sellerHex, Balance: 100})

	ctx := newCtx(t, state, buyerHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxBuyMarket
	payload, _ := json.Marshal(core.BuyMarketPayload{ListingID: "l1_ne"})

	if err := handleBuyMarket(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}

func TestCancelListing_NilEmitter(t *testing.T) {
	state := testutil.NewStateDB()
	_, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()

	_ = state.SetAsset(&core.Asset{ID: "a1_ne", Owner: pubHex, Tradeable: true, ActiveListingID: "l1_ne", Properties: map[string]any{}})
	_ = state.SetListing(&core.MarketListing{ID: "l1_ne", AssetID: "a1_ne", Seller: pubHex, Price: 100, Active: true})

	ctx := newCtx(t, state, pubHex)
	ctx.Emitter = nil
	ctx.Tx.Type = core.TxCancelListing
	payload, _ := json.Marshal(core.CancelListingPayload{ListingID: "l1_ne"})

	if err := handleCancelListing(ctx, payload); err != nil {
		t.Fatalf("expected success with nil emitter: %v", err)
	}
}
