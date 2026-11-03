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

func handleListMarket(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.ListMarketPayload](payload, core.TxListMarket)
	if err != nil {
		return err
	}
	if p.Price == 0 {
		return errors.New("price must be > 0")
	}

	asset, err := ctx.State.GetAsset(p.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", p.AssetID, err)
	}
	if asset.Owner != ctx.EffectiveSender {
		return fmt.Errorf("only the asset owner can list it: %w", core.ErrUnauthorized)
	}
	if !asset.Tradeable {
		return errors.New("asset is not tradeable")
	}
	if asset.Equipped() {
		return fmt.Errorf("asset %q is equipped; unequip it before listing", p.AssetID)
	}
	if asset.Listed() {
		return fmt.Errorf("asset %q is already listed (listing %s): %w", p.AssetID, asset.ActiveListingID, core.ErrAlreadyExists)
	}

	listingID := crypto.Hash([]byte(ctx.Tx.ID + ":listing:" + p.AssetID))
	listing := &core.MarketListing{
		ID:        listingID,
		AssetID:   p.AssetID,
		Seller:    ctx.EffectiveSender,
		Price:     p.Price,
		Active:    true,
		CreatedAt: ctx.Block.Header.Timestamp,
	}
	if err := ctx.State.SetListing(listing); err != nil {
		return err
	}
	asset.ActiveListingID = listingID
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	ctx.Emit(events.EventMarketList, events.MarketListData{ListingID: listingID, AssetID: p.AssetID, Price: p.Price})
	return nil
}

func handleBuyMarket(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.BuyMarketPayload](payload, core.TxBuyMarket)
	if err != nil {
		return err
	}

	listing, err := ctx.State.GetListing(p.ListingID)
	if err != nil {
		return fmt.Errorf("listing %q not found: %w", p.ListingID, err)
	}
	if !listing.Active {
		return fmt.Errorf("listing %q is no longer active", p.ListingID)
	}
	if listing.Seller == ctx.EffectiveSender {
		return fmt.Errorf("seller cannot buy their own listing: %w", core.ErrUnauthorized)
	}
	if err := vm.ChargeDelegationAmount(ctx, listing.Price); err != nil {
		return err
	}

	buyer, err := ctx.State.GetAccount(ctx.EffectiveSender)
	if err != nil {
		return err
	}
	if buyer.Balance < listing.Price {
		return fmt.Errorf("insufficient balance: have %d need %d: %w", buyer.Balance, listing.Price, core.ErrInsufficientBalance)
	}
	buyer.Balance -= listing.Price
	if err := ctx.State.SetAccount(buyer); err != nil {
		return err
	}

	seller, err := ctx.State.GetAccount(listing.Seller)
	if err != nil {
		return err
	}
	newBal, err := vm.SafeAdd(seller.Balance, listing.Price)
	if err != nil {
		return fmt.Errorf("seller balance overflow: %w", err)
	}
	seller.Balance = newBal
	if err := ctx.State.SetAccount(seller); err != nil {
		return err
	}

	asset, err := ctx.State.GetAsset(listing.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", listing.AssetID, err)
	}
	asset.Owner = ctx.EffectiveSender
	asset.ActiveListingID = ""
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	listing.Active = false
	if err := ctx.State.SetListing(listing); err != nil {
		return err
	}

	ctx.Emit(events.EventMarketBuy, events.MarketBuyData{
		ListingID: p.ListingID,
		AssetID:   listing.AssetID,
		Buyer:     ctx.EffectiveSender,
		Seller:    listing.Seller,
		Price:     listing.Price,
	})
	return nil
}

func handleCancelListing(ctx *vm.Context, payload json.RawMessage) error {
	p, err := vm.DecodePayload[core.CancelListingPayload](payload, core.TxCancelListing)
	if err != nil {
		return err
	}

	listing, err := ctx.State.GetListing(p.ListingID)
	if err != nil {
		return fmt.Errorf("listing %q not found: %w", p.ListingID, err)
	}
	if !listing.Active {
		return fmt.Errorf("listing %q is not active", p.ListingID)
	}
	if listing.Seller != ctx.EffectiveSender {
		return fmt.Errorf("only the seller can cancel a listing: %w", core.ErrUnauthorized)
	}

	listing.Active = false
	if err := ctx.State.SetListing(listing); err != nil {
		return err
	}
	asset, err := ctx.State.GetAsset(listing.AssetID)
	if err != nil {
		return fmt.Errorf("asset %q not found: %w", listing.AssetID, err)
	}
	asset.ActiveListingID = ""
	if err := ctx.State.SetAsset(asset); err != nil {
		return err
	}

	ctx.Emit(events.EventMarketCancel, events.MarketCancelData{ListingID: p.ListingID, AssetID: listing.AssetID, Seller: listing.Seller})
	return nil
}
