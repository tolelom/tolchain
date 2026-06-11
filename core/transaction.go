package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tolelom/tolchain/crypto"
)

// TxType identifies the kind of operation a transaction performs.
type TxType string

const (
	TxTransfer         TxType = "transfer"
	TxMintAsset        TxType = "mint_asset"
	TxBurnAsset        TxType = "burn_asset"
	TxTransferAsset    TxType = "transfer_asset"
	TxRegisterTemplate TxType = "register_template"
	TxSessionOpen      TxType = "session_open"
	TxSessionResult    TxType = "session_result"
	TxListMarket       TxType = "list_market"
	TxBuyMarket        TxType = "buy_market"
	TxCancelListing    TxType = "cancel_listing"
	TxSessionCancel    TxType = "session_cancel"
	TxEquipItem        TxType = "equip_item"
	TxUnequipItem      TxType = "unequip_item"
	TxGrantReward      TxType = "grant_reward"
	TxRandomCommit     TxType = "random_commit"
	TxRandomReveal     TxType = "random_reveal"
	TxGrantDelegation  TxType = "grant_delegation"
	TxRevokeDelegation TxType = "revoke_delegation"
)

// Transaction is the atomic unit of work on the chain.
// From holds the sender's full hex-encoded ed25519 public key (64 chars).
// ChainID prevents replay of this transaction on a different network.
// Signature covers all fields except Signature itself.
type Transaction struct {
	ID        string          `json:"id"`
	ChainID   string          `json:"chain_id"` // must match the receiving node's chain ID
	Type      TxType          `json:"type"`
	From      string          `json:"from"`      // hex-encoded ed25519 public key
	Nonce     uint64          `json:"nonce"`
	Fee       uint64          `json:"fee"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	Signature  string          `json:"signature"`
	OnBehalfOf string          `json:"on_behalf_of,omitempty"`
}

// signingBody holds the fields that are covered by the signature.
type signingBody struct {
	ChainID   string          `json:"chain_id"`
	Type      TxType          `json:"type"`
	From      string          `json:"from"`
	Nonce     uint64          `json:"nonce"`
	Fee       uint64          `json:"fee"`
	Timestamp int64           `json:"timestamp"`
	Payload    json.RawMessage `json:"payload"`
	OnBehalfOf string          `json:"on_behalf_of,omitempty"`
}

// Hash returns a deterministic hash of the transaction (sans Signature).
// Panics if marshalling fails (which cannot happen in practice).
func (tx *Transaction) Hash() string {
	body := signingBody{
		ChainID:    tx.ChainID,
		Type:       tx.Type,
		From:       tx.From,
		Nonce:      tx.Nonce,
		Fee:        tx.Fee,
		Timestamp:  tx.Timestamp,
		Payload:    tx.Payload,
		OnBehalfOf: tx.OnBehalfOf,
	}
	data, err := json.Marshal(body)
	if err != nil {
		panic("transaction hash marshal failed: " + err.Error())
	}
	return crypto.Hash(data)
}

// Sign computes the signature and sets ID.
func (tx *Transaction) Sign(priv crypto.PrivateKey) {
	hash := tx.Hash()
	tx.Signature = crypto.Sign(priv, []byte(hash))
	tx.ID = hash
}

// Verify checks the signature, that From is a valid public key, and that
// tx.ID matches the recomputed hash. This prevents a transaction whose ID
// was tampered with from being accepted into the mempool or a block.
func (tx *Transaction) Verify() error {
	if tx.From == "" {
		return errors.New("missing from field")
	}
	pub, err := crypto.PubKeyFromHex(tx.From)
	if err != nil {
		return fmt.Errorf("invalid from (must be ed25519 pubkey hex): %w", err)
	}
	hash := tx.Hash()
	if tx.ID != hash {
		return fmt.Errorf("tx ID mismatch: declared %s computed %s", tx.ID, hash)
	}
	return crypto.Verify(pub, []byte(hash), tx.Signature)
}

// NewTransaction creates an unsigned transaction with the current timestamp.
// chainID must match the target network (e.g. "tolchain-dev") to prevent
// cross-chain replay attacks.
func NewTransaction(chainID string, typ TxType, from string, nonce, fee uint64, payload any) (*Transaction, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return &Transaction{
		ChainID:   chainID,
		Type:      typ,
		From:      from,
		Nonce:     nonce,
		Fee:       fee,
		Timestamp: time.Now().UnixNano(),
		Payload:   raw,
	}, nil
}

// ---- Payload types ----

// TransferPayload transfers native tokens.
type TransferPayload struct {
	To     string `json:"to"`
	Amount uint64 `json:"amount"`
}

// MintAssetPayload mints a new asset from a registered template.
type MintAssetPayload struct {
	TemplateID string         `json:"template_id"`
	Owner      string         `json:"owner"`       // recipient pubkey hex
	Properties map[string]any `json:"properties"`
}

// BurnAssetPayload permanently destroys an asset.
type BurnAssetPayload struct {
	AssetID string `json:"asset_id"`
}

// TransferAssetPayload moves an asset to a new owner.
type TransferAssetPayload struct {
	AssetID string `json:"asset_id"`
	To      string `json:"to"` // recipient pubkey hex
}

// RegisterTemplatePayload defines a new class of game assets.
type RegisterTemplatePayload struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Schema    map[string]any `json:"schema"`    // allowed property keys → type hints
	Tradeable bool           `json:"tradeable"`
}

// SessionOpenPayload opens a new game session and locks stakes.
// When Stakes > 0, Signatures must contain each player's (except tx.From)
// ed25519 signature over "session:<SessionID>" to prove consent.
type SessionOpenPayload struct {
	SessionID  string            `json:"session_id"`
	GameID     string            `json:"game_id"`
	Players    []string          `json:"players"`              // participant pubkey hexes
	Stakes     uint64            `json:"stakes"`               // tokens locked per player
	Signatures map[string]string `json:"signatures,omitempty"` // pubkey hex → signature hex
}

// SessionResultPayload closes a session and distributes rewards.
type SessionResultPayload struct {
	SessionID string            `json:"session_id"`
	Outcome   map[string]uint64 `json:"outcome"` // pubkey hex → reward
}

// ListMarketPayload lists an asset for sale.
type ListMarketPayload struct {
	AssetID string `json:"asset_id"`
	Price   uint64 `json:"price"`
}

// BuyMarketPayload purchases an active market listing.
type BuyMarketPayload struct {
	ListingID string `json:"listing_id"`
}

// CancelListingPayload cancels an active market listing.
type CancelListingPayload struct {
	ListingID string `json:"listing_id"`
}

// SessionCancelPayload cancels an open session and refunds stakes.
type SessionCancelPayload struct {
	SessionID string `json:"session_id"`
}

// EquipItemPayload equips an asset to a named slot.
type EquipItemPayload struct {
	AssetID string `json:"asset_id"`
	Slot    string `json:"slot"`
}

// UnequipItemPayload removes an asset from its equipped slot.
type UnequipItemPayload struct {
	AssetID string `json:"asset_id"`
}

// GrantRewardPayload atomically grants tokens and mints assets. Operator-only.
type GrantRewardPayload struct {
	Recipient   string           `json:"recipient"`
	TokenAmount uint64           `json:"token_amount"`
	Assets      []MintAssetPayload `json:"assets"`
}

// RandomCommitPayload commits a hash for the commit-reveal random scheme.
type RandomCommitPayload struct {
	CommitID   string `json:"commit_id"`
	CommitHash string `json:"commit_hash"` // hex SHA-256 of secret
}

// RandomRevealPayload reveals the secret for a previous commitment.
type RandomRevealPayload struct {
	CommitID string `json:"commit_id"`
	Secret   string `json:"secret"`
}

// GrantDelegationPayload is the payload for grant_delegation transactions.
type GrantDelegationPayload struct {
	Grantee    string   `json:"grantee"`
	AllowTypes []string `json:"allow_types"`
	ExpiresAt  int64    `json:"expires_at"`
	MaxUses    uint64   `json:"max_uses"`
	MaxAmount  uint64   `json:"max_amount"`
}

// RevokeDelegationPayload is the payload for revoke_delegation transactions.
type RevokeDelegationPayload struct {
	Grantee string `json:"grantee"`
}

// DelegationGrant is the state record for an active delegation.
type DelegationGrant struct {
	Granter     string   `json:"granter"`
	Grantee     string   `json:"grantee"`
	AllowTypes  []string `json:"allow_types"`
	ExpiresAt   int64    `json:"expires_at"`
	MaxUses     uint64   `json:"max_uses"`
	UsedCount   uint64   `json:"used_count"`
	MaxAmount   uint64   `json:"max_amount"`
	SpentAmount uint64   `json:"spent_amount"`
	CreatedAt   int64    `json:"created_at"`
}
