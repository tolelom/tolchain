package market

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

func init() {
	vm.Register(core.TxListMarket, handleListMarket)
	vm.Register(core.TxBuyMarket, handleBuyMarket)
}

func handleListMarket(ctx *vm.Context, payload json.RawMessage) error {
	var p core.ListMarketPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode list_market payload: %w", err)
	}
	if p.Price == 0 {
		return errors.New("price must be > 0")
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.Tx.From {
		return errors.New("only the asset owner can list it")
	}
	if !asset.Tradeable {
		return errors.New("asset is not tradeable")
	}
	// Prevent double-listing the same asset.
	if asset.ActiveListingID != "" {
		return fmt.Errorf("asset %q is already listed (listing %s)", p.AssetID, asset.ActiveListingID)
	}

	listingID := crypto.Hash([]byte(ctx.Tx.ID + ":listing:" + p.AssetID))

	listing := &core.MarketListing{
		ID:        listingID,
		AssetID:   p.AssetID,
		Seller:    ctx.Tx.From,
		Price:     p.Price,
		Active:    true,
		CreatedAt: ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetListing(listing); err != nil {
		return err
	}

	// Mark the asset as having an active listing so it cannot be listed again.
	asset.ActiveListingID = listingID
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventMarketList,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data:        map[string]any{"listing_id": listingID, "asset_id": p.AssetID, "price": p.Price},
		})
	}
	return nil
}

func handleBuyMarket(ctx *vm.Context, payload json.RawMessage) error {
	var p core.BuyMarketPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode buy_market payload: %w", err)
	}

	listing, err := ctx.State.GetListing(p.ListingID)
	if err != nil {
		return fmt.Errorf("listing %q not found: %w", p.ListingID, err)
	}
	if !listing.Active {
		return fmt.Errorf("listing %q is no longer active", p.ListingID)
	}
	if listing.Seller == ctx.Tx.From {
		return errors.New("seller cannot buy their own listing")
	}

	// Deduct price from buyer
	buyer, err := ctx.State.GetAccount(ctx.Tx.From)
	if err != nil {
		return err
	}
	if buyer.Balance < listing.Price {
		return fmt.Errorf("insufficient balance: have %d need %d", buyer.Balance, listing.Price)
	}
	buyer.Balance -= listing.Price
	if err := ctx.State.SetAccount(buyer); err != nil {
		return err
	}

	// Credit seller
	seller, err := ctx.State.GetAccount(listing.Seller)
	if err != nil {
		return err
	}
	seller.Balance += listing.Price
	if err := ctx.State.SetAccount(seller); err != nil {
		return err
	}

	// Transfer asset and clear its active listing marker.
	asset, err := ctx.State.GetAsset(listing.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", listing.AssetID, err)
	}
	asset.Owner = ctx.Tx.From
	asset.ActiveListingID = ""
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	// Deactivate listing
	listing.Active = false
	if err := ctx.State.SetListing(listing); err != nil {
		return err
	}

	if ctx.Emitter != nil {
		ctx.Emitter.Emit(events.Event{
			Type:        events.EventMarketBuy,
			TxID:        ctx.Tx.ID,
			BlockHeight: ctx.Block.Header.Height,
			Data: map[string]any{
				"listing_id": p.ListingID,
				"asset_id":   listing.AssetID,
				"buyer":      ctx.Tx.From,
				"seller":     listing.Seller,
				"price":      listing.Price,
			},
		})
	}
	return nil
}
