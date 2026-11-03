package wallet

import (
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

type Wallet struct {
	priv crypto.PrivateKey
	pub  crypto.PublicKey
}

func New(priv crypto.PrivateKey) *Wallet {
	return &Wallet{priv: priv, pub: priv.Public()}
}

func Generate() (*Wallet, error) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return New(priv), nil
}

func (w *Wallet) PrivKey() crypto.PrivateKey {
	return w.priv
}

// PubKey is the hex public key, the on-chain account identity.
func (w *Wallet) PubKey() string {
	return w.pub.Hex()
}

func (w *Wallet) Address() string {
	return w.pub.Address()
}

// NewTx signs a tx; nonce must be the account's next nonce.
func (w *Wallet) NewTx(chainID string, typ core.TxType, nonce, fee uint64, payload any) (*core.Transaction, error) {
	tx, err := core.NewTransaction(chainID, typ, w.pub.Hex(), nonce, fee, payload)
	if err != nil {
		return nil, err
	}
	tx.Sign(w.priv)
	return tx, nil
}

func (w *Wallet) Transfer(chainID, to string, amount, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxTransfer, nonce, fee, core.TransferPayload{
		To:     to,
		Amount: amount,
	})
}

func (w *Wallet) MintAsset(chainID, templateID, owner string, properties map[string]any, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxMintAsset, nonce, fee, core.MintAssetPayload{
		TemplateID: templateID,
		Owner:      owner,
		Properties: properties,
	})
}

func (w *Wallet) BurnAsset(chainID, assetID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxBurnAsset, nonce, fee, core.BurnAssetPayload{
		AssetID: assetID,
	})
}

func (w *Wallet) TransferAsset(chainID, assetID, to string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxTransferAsset, nonce, fee, core.TransferAssetPayload{
		AssetID: assetID,
		To:      to,
	})
}

func (w *Wallet) RegisterTemplate(chainID, id, name string, schema map[string]any, tradeable bool, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRegisterTemplate, nonce, fee, core.RegisterTemplatePayload{
		ID:        id,
		Name:      name,
		Schema:    schema,
		Tradeable: tradeable,
	})
}

func (w *Wallet) ListMarket(chainID, assetID string, price, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxListMarket, nonce, fee, core.ListMarketPayload{
		AssetID: assetID,
		Price:   price,
	})
}

func (w *Wallet) BuyMarket(chainID, listingID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxBuyMarket, nonce, fee, core.BuyMarketPayload{
		ListingID: listingID,
	})
}

func (w *Wallet) CancelListing(chainID, listingID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxCancelListing, nonce, fee, core.CancelListingPayload{
		ListingID: listingID,
	})
}

func (w *Wallet) EquipItem(chainID, assetID, slot string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxEquipItem, nonce, fee, core.EquipItemPayload{
		AssetID: assetID,
		Slot:    slot,
	})
}

func (w *Wallet) UnequipItem(chainID, assetID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxUnequipItem, nonce, fee, core.UnequipItemPayload{
		AssetID: assetID,
	})
}

func (w *Wallet) GrantReward(chainID, recipient string, tokenAmount uint64, assets []core.MintAssetPayload, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxGrantReward, nonce, fee, core.GrantRewardPayload{
		Recipient:   recipient,
		TokenAmount: tokenAmount,
		Assets:      assets,
	})
}

func (w *Wallet) RandomCommit(chainID, commitID, commitHash string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRandomCommit, nonce, fee, core.RandomCommitPayload{
		CommitID:   commitID,
		CommitHash: commitHash,
	})
}

func (w *Wallet) RandomReveal(chainID, commitID, secret string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRandomReveal, nonce, fee, core.RandomRevealPayload{
		CommitID: commitID,
		Secret:   secret,
	})
}

func (w *Wallet) GrantDelegation(chainID, grantee string, allowTypes []string, expiresAt int64, maxUses, maxAmount, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxGrantDelegation, nonce, fee, core.GrantDelegationPayload{
		Grantee:    grantee,
		AllowTypes: allowTypes,
		ExpiresAt:  expiresAt,
		MaxUses:    maxUses,
		MaxAmount:  maxAmount,
	})
}

func (w *Wallet) RevokeDelegation(chainID, grantee string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRevokeDelegation, nonce, fee, core.RevokeDelegationPayload{
		Grantee: grantee,
	})
}

// NewDelegatedTx signs as grantee for the granter onBehalfOf; nonce is the
// granter's.
func (w *Wallet) NewDelegatedTx(chainID string, typ core.TxType, onBehalfOf string, nonce, fee uint64, payload any) (*core.Transaction, error) {
	tx, err := core.NewTransaction(chainID, typ, w.pub.Hex(), nonce, fee, payload)
	if err != nil {
		return nil, err
	}
	tx.OnBehalfOf = onBehalfOf
	tx.Sign(w.priv)
	return tx, nil
}
