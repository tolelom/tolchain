package wallet

import (
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// Wallet holds a key pair and provides transaction-building helpers.
type Wallet struct {
	priv crypto.PrivateKey
	pub  crypto.PublicKey
}

// New creates a Wallet from an existing private key.
func New(priv crypto.PrivateKey) *Wallet {
	return &Wallet{priv: priv, pub: priv.Public()}
}

// Generate creates a Wallet with a freshly generated key pair.
func Generate() (*Wallet, error) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	return New(priv), nil
}

// PrivKey returns the raw private key (handle with care).
func (w *Wallet) PrivKey() crypto.PrivateKey {
	return w.priv
}

// PubKey returns the hex-encoded ed25519 public key (used as "from" address).
func (w *Wallet) PubKey() string {
	return w.pub.Hex()
}

// Address returns the short human-readable address (first 20 bytes of SHA-256(pubkey)).
func (w *Wallet) Address() string {
	return w.pub.Address()
}

// NewTx creates a signed transaction. chainID must match the target network.
// nonce should match the account's current nonce.
func (w *Wallet) NewTx(chainID string, typ core.TxType, nonce, fee uint64, payload any) (*core.Transaction, error) {
	tx, err := core.NewTransaction(chainID, typ, w.pub.Hex(), nonce, fee, payload)
	if err != nil {
		return nil, err
	}
	tx.Sign(w.priv)
	return tx, nil
}

// Transfer creates a signed transfer transaction.
func (w *Wallet) Transfer(chainID, to string, amount, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxTransfer, nonce, fee, core.TransferPayload{
		To:     to,
		Amount: amount,
	})
}

// MintAsset creates a signed mint_asset transaction.
func (w *Wallet) MintAsset(chainID, templateID, owner string, properties map[string]any, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxMintAsset, nonce, fee, core.MintAssetPayload{
		TemplateID: templateID,
		Owner:      owner,
		Properties: properties,
	})
}

// BurnAsset creates a signed burn_asset transaction.
func (w *Wallet) BurnAsset(chainID, assetID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxBurnAsset, nonce, fee, core.BurnAssetPayload{
		AssetID: assetID,
	})
}

// TransferAsset creates a signed transfer_asset transaction.
func (w *Wallet) TransferAsset(chainID, assetID, to string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxTransferAsset, nonce, fee, core.TransferAssetPayload{
		AssetID: assetID,
		To:      to,
	})
}

// RegisterTemplate creates a signed register_template transaction.
func (w *Wallet) RegisterTemplate(chainID, id, name string, schema map[string]any, tradeable bool, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRegisterTemplate, nonce, fee, core.RegisterTemplatePayload{
		ID:        id,
		Name:      name,
		Schema:    schema,
		Tradeable: tradeable,
	})
}

// ListMarket creates a signed list_market transaction.
func (w *Wallet) ListMarket(chainID, assetID string, price, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxListMarket, nonce, fee, core.ListMarketPayload{
		AssetID: assetID,
		Price:   price,
	})
}

// BuyMarket creates a signed buy_market transaction.
func (w *Wallet) BuyMarket(chainID, listingID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxBuyMarket, nonce, fee, core.BuyMarketPayload{
		ListingID: listingID,
	})
}

// CancelListing creates a signed cancel_listing transaction.
func (w *Wallet) CancelListing(chainID, listingID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxCancelListing, nonce, fee, core.CancelListingPayload{
		ListingID: listingID,
	})
}

// EquipItem creates a signed equip_item transaction.
func (w *Wallet) EquipItem(chainID, assetID, slot string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxEquipItem, nonce, fee, core.EquipItemPayload{
		AssetID: assetID,
		Slot:    slot,
	})
}

// UnequipItem creates a signed unequip_item transaction.
func (w *Wallet) UnequipItem(chainID, assetID string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxUnequipItem, nonce, fee, core.UnequipItemPayload{
		AssetID: assetID,
	})
}

// GrantReward creates a signed grant_reward transaction.
func (w *Wallet) GrantReward(chainID, recipient string, tokenAmount uint64, assets []core.MintAssetPayload, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxGrantReward, nonce, fee, core.GrantRewardPayload{
		Recipient:   recipient,
		TokenAmount: tokenAmount,
		Assets:      assets,
	})
}

// RandomCommit creates a signed random_commit transaction.
func (w *Wallet) RandomCommit(chainID, commitID, commitHash string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRandomCommit, nonce, fee, core.RandomCommitPayload{
		CommitID:   commitID,
		CommitHash: commitHash,
	})
}

// RandomReveal creates a signed random_reveal transaction.
func (w *Wallet) RandomReveal(chainID, commitID, secret string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRandomReveal, nonce, fee, core.RandomRevealPayload{
		CommitID: commitID,
		Secret:   secret,
	})
}

// GrantDelegation creates a signed grant_delegation transaction.
func (w *Wallet) GrantDelegation(chainID, grantee string, allowTypes []string, expiresAt int64, maxUses, maxAmount, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxGrantDelegation, nonce, fee, core.GrantDelegationPayload{
		Grantee:    grantee,
		AllowTypes: allowTypes,
		ExpiresAt:  expiresAt,
		MaxUses:    maxUses,
		MaxAmount:  maxAmount,
	})
}

// RevokeDelegation creates a signed revoke_delegation transaction.
func (w *Wallet) RevokeDelegation(chainID, grantee string, nonce, fee uint64) (*core.Transaction, error) {
	return w.NewTx(chainID, core.TxRevokeDelegation, nonce, fee, core.RevokeDelegationPayload{
		Grantee: grantee,
	})
}

// NewDelegatedTx creates a signed transaction on behalf of another account (delegation).
// The wallet signs as grantee, while onBehalfOf specifies the granter's pubkey.
func (w *Wallet) NewDelegatedTx(chainID string, typ core.TxType, onBehalfOf string, nonce, fee uint64, payload any) (*core.Transaction, error) {
	tx, err := core.NewTransaction(chainID, typ, w.pub.Hex(), nonce, fee, payload)
	if err != nil {
		return nil, err
	}
	tx.OnBehalfOf = onBehalfOf
	tx.Sign(w.priv)
	return tx, nil
}
