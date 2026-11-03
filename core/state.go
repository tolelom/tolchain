package core

// Every address-like field in state (Address, Owner, Creator, Seller, players,
// Committer, Granter, Grantee) is a hex-encoded ed25519 public key.

// TotalSupplyAddress is the pseudo-account whose balance tracks minted
// tokens against Config.MaxTotalSupply. It is not a public key, so no one can
// sign for it.
const TotalSupplyAddress = "system:total_supply"

type Account struct {
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

// Asset is a game item, card, character and so on. Properties is open so each
// game can store its own fields.
type Asset struct {
	ID              string         `json:"id"`
	TemplateID      string         `json:"template_id"`
	Owner           string         `json:"owner"`
	Properties      map[string]any `json:"properties"`
	Tradeable       bool           `json:"tradeable"`
	MintedAt        int64          `json:"minted_at"`
	ActiveListingID string         `json:"active_listing_id,omitempty"`
}

// Properties the inventory module writes into Asset.Properties while an asset
// is equipped.
const (
	AssetPropEquipped = "equipped"
	AssetPropSlot     = "slot"
)

func (a *Asset) Equipped() bool {
	equipped, _ := a.Properties[AssetPropEquipped].(bool)
	return equipped
}

func (a *Asset) Listed() bool { return a.ActiveListingID != "" }

type AssetTemplate struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Schema    map[string]any `json:"schema"` // property key → type hint ("string", "int", "bool")
	Tradeable bool           `json:"tradeable"`
	Creator   string         `json:"creator"`
}

const (
	SessionStatusOpen      = "open"
	SessionStatusClosed    = "closed"
	SessionStatusCancelled = "cancelled"
)

type Session struct {
	ID        string            `json:"id"`
	GameID    string            `json:"game_id"`
	Creator   string            `json:"creator"`
	Players   []string          `json:"players"`
	Stakes    uint64            `json:"stakes"` // per player
	Status    string            `json:"status"`
	Outcome   map[string]uint64 `json:"outcome"` // player → reward
	CreatedAt int64             `json:"created_at"`
	ClosedAt  int64             `json:"closed_at"`
}

type MarketListing struct {
	ID        string `json:"id"`
	AssetID   string `json:"asset_id"`
	Seller    string `json:"seller"`
	Price     uint64 `json:"price"`
	Active    bool   `json:"active"`
	CreatedAt int64  `json:"created_at"`
}

type Inventory struct {
	Owner string            `json:"owner"`
	Slots map[string]string `json:"slots"` // slot name → asset ID
}

type RandomCommitment struct {
	ID          string `json:"id"`
	Committer   string `json:"committer"`
	CommitHash  string `json:"commit_hash"` // hex SHA-256 of the secret
	BlockHeight int64  `json:"block_height"`
	Revealed    bool   `json:"revealed"`
	Secret      string `json:"secret,omitempty"`
	Result      string `json:"result,omitempty"`
}

// State is the world state seen by the executor and RPC.
type State interface {
	GetAccount(address string) (*Account, error)
	SetAccount(account *Account) error

	GetAsset(id string) (*Asset, error)
	SetAsset(asset *Asset) error
	DeleteAsset(id string) error

	GetTemplate(id string) (*AssetTemplate, error)
	SetTemplate(t *AssetTemplate) error

	GetSession(id string) (*Session, error)
	SetSession(s *Session) error

	GetListing(id string) (*MarketListing, error)
	SetListing(l *MarketListing) error

	GetInventory(owner string) (*Inventory, error)
	SetInventory(inv *Inventory) error

	GetRandomCommitment(id string) (*RandomCommitment, error)
	SetRandomCommitment(rc *RandomCommitment) error

	GetDelegation(granter, grantee string) (*DelegationGrant, error)
	SetDelegation(grant *DelegationGrant) error
	DeleteDelegation(granter, grantee string) error
	GetDelegationsByGranter(granter string) ([]*DelegationGrant, error)
	GetDelegationsByGrantee(grantee string) ([]*DelegationGrant, error)

	Snapshot() (int, error)
	RevertToSnapshot(id int) error
	// ComputeRoot hashes the state including uncommitted writes. On error the
	// caller must abort the block: the root would cover partial state.
	ComputeRoot() (string, error)
	// Commit persists uncommitted writes; call it only after the block that
	// carries ComputeRoot's result is stored.
	Commit() error

	// BlockLock is held by block producers and appliers for the whole
	// execute-commit cycle; RPC reads take BlockRLock so they never observe a
	// half-applied block.
	BlockLock()
	BlockUnlock()
	BlockRLock()
	BlockRUnlock()
}
