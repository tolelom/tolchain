package core

// Account holds a participant's token balance and replay-protection nonce.
// Address is the hex-encoded ed25519 public key.
type Account struct {
	Address string `json:"address"` // pubkey hex
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

// Asset is a universal game asset: item, card, character, etc.
// Properties is an open map so each game genre can store arbitrary fields.
type Asset struct {
	ID              string         `json:"id"`
	TemplateID      string         `json:"template_id"`
	Owner           string         `json:"owner"`             // pubkey hex
	Properties      map[string]any `json:"properties"`
	Tradeable       bool           `json:"tradeable"`
	MintedAt        int64          `json:"minted_at"`
	ActiveListingID string         `json:"active_listing_id,omitempty"` // non-empty while listed
}

// AssetTemplate defines the schema and rules for a class of assets.
type AssetTemplate struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Schema    map[string]any `json:"schema"` // property key → type hint
	Tradeable bool           `json:"tradeable"`
	Creator   string         `json:"creator"` // pubkey hex of registrant
}

// Session represents an active or completed game match.
type Session struct {
	ID        string            `json:"id"`
	GameID    string            `json:"game_id"`
	Creator   string            `json:"creator"`  // pubkey hex of the session opener
	Players   []string          `json:"players"`  // pubkey hexes
	Stakes    uint64            `json:"stakes"`   // tokens locked per player
	Status    string            `json:"status"`   // "open" | "closed"
	Outcome   map[string]uint64 `json:"outcome"`  // pubkey hex → reward
	CreatedAt int64             `json:"created_at"`
	ClosedAt  int64             `json:"closed_at"`
}

// MarketListing is a P2P asset sale offer.
type MarketListing struct {
	ID        string `json:"id"`
	AssetID   string `json:"asset_id"`
	Seller    string `json:"seller"`     // pubkey hex
	Price     uint64 `json:"price"`
	Active    bool   `json:"active"`
	CreatedAt int64  `json:"created_at"`
}

// State is the full blockchain state interface. Implementations must be
// snapshot-able so the executor can roll back failed transactions.
type State interface {
	// Accounts
	GetAccount(address string) (*Account, error)
	SetAccount(account *Account) error

	// Assets
	GetAsset(id string) (*Asset, error)
	SetAsset(asset *Asset) error
	DeleteAsset(id string) error

	// Templates
	GetTemplate(id string) (*AssetTemplate, error)
	SetTemplate(t *AssetTemplate) error

	// Sessions
	GetSession(id string) (*Session, error)
	SetSession(s *Session) error

	// Market
	GetListing(id string) (*MarketListing, error)
	SetListing(l *MarketListing) error

	// Snapshot / rollback / commit
	Snapshot() (int, error)
	RevertToSnapshot(id int) error
	// ComputeRoot returns the deterministic state root from the current write
	// buffer without flushing. Call this before signing a block.
	ComputeRoot() string
	// Commit flushes the write buffer to the underlying DB and clears it.
	// Always call ComputeRoot() first to obtain the root for the block header.
	Commit() error
}
