package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tolelom/tolchain/crypto"
)

// TxType and its constants are generated from design/tolchain.yaml.

// Transaction is signed by From (an ed25519 pubkey hex). OnBehalfOf names the
// account it acts for under a delegation; its nonce and balance are used.
type Transaction struct {
	ID         string          `json:"id"`
	ChainID    string          `json:"chain_id"`
	Type       TxType          `json:"type"`
	From       string          `json:"from"`
	Nonce      uint64          `json:"nonce"`
	Fee        uint64          `json:"fee"`
	Timestamp  int64           `json:"timestamp"`
	Payload    json.RawMessage `json:"payload"`
	Signature  string          `json:"signature"`
	OnBehalfOf string          `json:"on_behalf_of,omitempty"`
}

// signingBody is the signing input: every field but ID and Signature. Its
// field order and JSON tags are part of consensus; ChainID in it stops replay
// on other networks.
type signingBody struct {
	ChainID    string          `json:"chain_id"`
	Type       TxType          `json:"type"`
	From       string          `json:"from"`
	Nonce      uint64          `json:"nonce"`
	Fee        uint64          `json:"fee"`
	Timestamp  int64           `json:"timestamp"`
	Payload    json.RawMessage `json:"payload"`
	OnBehalfOf string          `json:"on_behalf_of,omitempty"`
}

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
		panic("transaction hash marshal failed: " + err.Error()) // unreachable for these field types
	}
	return crypto.Hash(data)
}

// Sign sets ID to the hash and signs it.
func (tx *Transaction) Sign(priv crypto.PrivateKey) {
	hash := tx.Hash()
	tx.Signature = crypto.Sign(priv, []byte(hash))
	tx.ID = hash
}

// Verify also requires ID to equal the recomputed hash, so a tx cannot enter
// the mempool or a block under a forged ID.
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

// NewTransaction returns an unsigned tx stamped with the current time.
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

type TransferPayload struct {
	To     string `json:"to"`
	Amount uint64 `json:"amount"`
}

type MintAssetPayload struct {
	TemplateID string         `json:"template_id"`
	Owner      string         `json:"owner"` // empty → the sender
	Properties map[string]any `json:"properties"`
}

type BurnAssetPayload struct {
	AssetID string `json:"asset_id"`
}

type TransferAssetPayload struct {
	AssetID string `json:"asset_id"`
	To      string `json:"to"`
}

type RegisterTemplatePayload struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Schema    map[string]any `json:"schema"`
	Tradeable bool           `json:"tradeable"`
}

// SessionOpenPayload needs, from every player except the sender, a signature
// over SessionConsentMessage(chainID, SessionID, GameID, Stakes).
type SessionOpenPayload struct {
	SessionID  string            `json:"session_id"`
	GameID     string            `json:"game_id"`
	Players    []string          `json:"players"`
	Stakes     uint64            `json:"stakes"`               // per player
	Signatures map[string]string `json:"signatures,omitempty"` // player → signature hex
}

type SessionResultPayload struct {
	SessionID string            `json:"session_id"`
	Outcome   map[string]uint64 `json:"outcome"` // player → reward; must sum to all stakes
}

type ListMarketPayload struct {
	AssetID string `json:"asset_id"`
	Price   uint64 `json:"price"`
}

type BuyMarketPayload struct {
	ListingID string `json:"listing_id"`
}

type CancelListingPayload struct {
	ListingID string `json:"listing_id"`
}

type SessionCancelPayload struct {
	SessionID string `json:"session_id"`
}

type EquipItemPayload struct {
	AssetID string `json:"asset_id"`
	Slot    string `json:"slot"`
}

type UnequipItemPayload struct {
	AssetID string `json:"asset_id"`
}

// GrantRewardPayload mints tokens and assets for Recipient in one tx.
type GrantRewardPayload struct {
	Recipient   string             `json:"recipient"`
	TokenAmount uint64             `json:"token_amount"`
	Assets      []MintAssetPayload `json:"assets"`
}

// A random result is SHA-256(secret + hash of the block RandomTargetDelay
// after the commit), revealable for RandomRevealWindow blocks after that.
const (
	RandomTargetDelay  = 2
	RandomRevealWindow = 100
)

type RandomCommitPayload struct {
	CommitID   string `json:"commit_id"`
	CommitHash string `json:"commit_hash"` // hex SHA-256 of the secret
}

type RandomRevealPayload struct {
	CommitID string `json:"commit_id"`
	Secret   string `json:"secret"`
}

// GrantDelegationPayload lets Grantee sign AllowTypes txs for the sender.
// ExpiresAt is Unix seconds compared with the block time; it and the
// limits mean "unlimited" when 0. MaxAmount caps tokens moved by transfer,
// buy_market and grant_reward.
type GrantDelegationPayload struct {
	Grantee    string   `json:"grantee"`
	AllowTypes []string `json:"allow_types"`
	ExpiresAt  int64    `json:"expires_at"`
	MaxUses    uint64   `json:"max_uses"`
	MaxAmount  uint64   `json:"max_amount"`
}

type RevokeDelegationPayload struct {
	Grantee string `json:"grantee"`
}

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
