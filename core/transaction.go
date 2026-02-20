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
)

// Transaction is the atomic unit of work on the chain.
// From holds the sender's full hex-encoded ed25519 public key (64 chars).
// Signature covers all fields except Signature itself.
type Transaction struct {
	ID        string          `json:"id"`
	Type      TxType          `json:"type"`
	From      string          `json:"from"`      // hex-encoded ed25519 public key
	Nonce     uint64          `json:"nonce"`
	Fee       uint64          `json:"fee"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

// signingBody holds the fields that are covered by the signature.
type signingBody struct {
	Type      TxType          `json:"type"`
	From      string          `json:"from"`
	Nonce     uint64          `json:"nonce"`
	Fee       uint64          `json:"fee"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// Hash returns a deterministic hash of the transaction (sans Signature).
// Returns an empty string if marshalling fails (which cannot happen in practice).
func (tx *Transaction) Hash() string {
	body := signingBody{
		Type:      tx.Type,
		From:      tx.From,
		Nonce:     tx.Nonce,
		Fee:       tx.Fee,
		Timestamp: tx.Timestamp,
		Payload:   tx.Payload,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return ""
	}
	return crypto.Hash(data)
}

// Sign computes the signature and sets ID.
func (tx *Transaction) Sign(priv crypto.PrivateKey) {
	hash := tx.Hash()
	tx.Signature = crypto.Sign(priv, []byte(hash))
	tx.ID = hash
}

// Verify checks the signature and that From is a valid public key.
func (tx *Transaction) Verify() error {
	if tx.From == "" {
		return errors.New("missing from field")
	}
	pub, err := crypto.PubKeyFromHex(tx.From)
	if err != nil {
		return fmt.Errorf("invalid from (must be ed25519 pubkey hex): %w", err)
	}
	return crypto.Verify(pub, []byte(tx.Hash()), tx.Signature)
}

// NewTransaction creates an unsigned transaction with the current timestamp.
func NewTransaction(typ TxType, from string, nonce, fee uint64, payload any) (*Transaction, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return &Transaction{
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
type SessionOpenPayload struct {
	SessionID string   `json:"session_id"`
	GameID    string   `json:"game_id"`
	Players   []string `json:"players"` // participant pubkey hexes
	Stakes    uint64   `json:"stakes"`  // tokens locked per player
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
