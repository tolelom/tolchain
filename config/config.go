package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

const (
	// DefaultRPCPort is the default JSON-RPC listen port.
	DefaultRPCPort = 8545
	// DefaultP2PPort is the default peer-to-peer listen port.
	DefaultP2PPort = 30303
	// DefaultMaxBlockTxs is the default maximum transactions per block.
	DefaultMaxBlockTxs = 500
	// MaxBlockTxsUpperBound is the hard cap for max_block_txs.
	MaxBlockTxsUpperBound = 10000
	// DefaultLogLevel is the default structured logging level.
	DefaultLogLevel = "info"
	// DefaultPruneKeepBlocks is the default number of recent blocks to keep.
	// 0 means no pruning (keep all blocks).
	DefaultPruneKeepBlocks = 0
)

// MempoolConfig controls transaction pool behavior.
type MempoolConfig struct {
	MaxSize      int `json:"max_size,omitempty"`       // max txs in pool; 0 → 10000
	MaxTxAgeSec  int `json:"max_tx_age_sec,omitempty"` // max past timestamp drift in seconds; 0 → 3600
	MaxFutureSec int `json:"max_future_sec,omitempty"` // max future timestamp drift in seconds; 0 → 300
}

// NetworkConfig controls P2P networking.
type NetworkConfig struct {
	MaxPeers         int `json:"max_peers,omitempty"`           // 0 → 50
	WriteTimeoutSec  int `json:"write_timeout_sec,omitempty"`   // 0 → 30
	ReadTimeoutSec   int `json:"read_timeout_sec,omitempty"`    // 0 → 300
	MaxMessageSize   int `json:"max_message_size,omitempty"`    // bytes; 0 → 10485760 (10MB)
	SyncBatchSize    int `json:"sync_batch_size,omitempty"`     // 0 → 50
	MaxSyncBatchSize int `json:"max_sync_batch_size,omitempty"` // 0 → 200
}

// RPCServerConfig controls JSON-RPC server behavior.
type RPCServerConfig struct {
	RateLimit      float64 `json:"rate_limit,omitempty"`        // requests/sec per IP; 0 → 100
	RateBurst      int     `json:"rate_burst,omitempty"`        // burst size; 0 → 200
	ReadTimeoutSec  int    `json:"read_timeout_sec,omitempty"`  // 0 → 30
	WriteTimeoutSec int    `json:"write_timeout_sec,omitempty"` // 0 → 30
	IdleTimeoutSec  int    `json:"idle_timeout_sec,omitempty"`  // 0 → 60
	MaxBodySize     int64  `json:"max_body_size,omitempty"`     // bytes; 0 → 1048576 (1MB)
	SSEBufferSize   int    `json:"sse_buffer_size,omitempty"`   // 0 → 64
}

// ConsensusConfig controls block production.
type ConsensusConfig struct {
	BlockIntervalSec int `json:"block_interval_sec,omitempty"` // 0 → 2
	MaxTimeDriftSec  int `json:"max_time_drift_sec,omitempty"` // 0 → 15
}

// TLSConfig holds paths to the PEM files needed for mTLS.
// When nil or all paths empty, the node falls back to plain TCP.
type TLSConfig struct {
	CACert   string `json:"ca_cert"`   // CA certificate PEM path
	NodeCert string `json:"node_cert"` // node certificate PEM path
	NodeKey  string `json:"node_key"`  // node private key PEM path
}

// SeedPeer identifies a remote node to connect to on startup.
type SeedPeer struct {
	ID   string `json:"id"`   // remote node ID
	Addr string `json:"addr"` // host:port
}

// GenesisConfig describes the chain's initial state.
type GenesisConfig struct {
	ChainID string            `json:"chain_id"`
	Alloc   map[string]uint64 `json:"alloc"` // pubkey hex → initial balance
}

// Config holds all node configuration.
type Config struct {
	NodeID      string        `json:"node_id"`
	DataDir     string        `json:"data_dir"`
	RPCPort     int           `json:"rpc_port"`
	P2PPort     int           `json:"p2p_port"`
	MaxBlockTxs int           `json:"max_block_txs"` // max transactions per block; 0 → 500
	Validators   []string      `json:"validators"`              // authorised proposer pubkey hexes
	Operators    []string      `json:"operators,omitempty"`     // authorised game operator pubkey hexes (empty → no restriction)
	Genesis      GenesisConfig `json:"genesis"`
	SeedPeers    []SeedPeer    `json:"seed_peers,omitempty"`     // initial peers to connect to
	TLS          *TLSConfig    `json:"tls,omitempty"`           // nil → plain TCP
	RPCAuthToken    string           `json:"rpc_auth_token,omitempty"`    // empty → no auth
	LogLevel        string           `json:"log_level,omitempty"`        // "debug","info","warn","error"; default "info"
	PruneKeepBlocks int64            `json:"prune_keep_blocks,omitempty"` // 0 → no pruning; >0 → keep only the latest N blocks
	MaxTotalSupply  uint64           `json:"max_total_supply,omitempty"` // 0 → unlimited; >0 → hard cap on total minted tokens
	Mempool         MempoolConfig    `json:"mempool,omitempty"`
	Network         NetworkConfig    `json:"network,omitempty"`
	RPC             RPCServerConfig  `json:"rpc,omitempty"`
	Consensus       ConsensusConfig  `json:"consensus,omitempty"`
}

// ApplyDefaults fills zero-valued fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.Mempool.MaxSize == 0 {
		c.Mempool.MaxSize = 10000
	}
	if c.Mempool.MaxTxAgeSec == 0 {
		c.Mempool.MaxTxAgeSec = 3600
	}
	if c.Mempool.MaxFutureSec == 0 {
		c.Mempool.MaxFutureSec = 300
	}
	if c.Network.MaxPeers == 0 {
		c.Network.MaxPeers = 50
	}
	if c.Network.WriteTimeoutSec == 0 {
		c.Network.WriteTimeoutSec = 30
	}
	if c.Network.ReadTimeoutSec == 0 {
		c.Network.ReadTimeoutSec = 300
	}
	if c.Network.MaxMessageSize == 0 {
		c.Network.MaxMessageSize = 10 * 1024 * 1024
	}
	if c.Network.SyncBatchSize == 0 {
		c.Network.SyncBatchSize = 50
	}
	if c.Network.MaxSyncBatchSize == 0 {
		c.Network.MaxSyncBatchSize = 200
	}
	if c.RPC.RateLimit == 0 {
		c.RPC.RateLimit = 100
	}
	if c.RPC.RateBurst == 0 {
		c.RPC.RateBurst = 200
	}
	if c.RPC.ReadTimeoutSec == 0 {
		c.RPC.ReadTimeoutSec = 30
	}
	if c.RPC.WriteTimeoutSec == 0 {
		c.RPC.WriteTimeoutSec = 30
	}
	if c.RPC.IdleTimeoutSec == 0 {
		c.RPC.IdleTimeoutSec = 60
	}
	if c.RPC.MaxBodySize == 0 {
		c.RPC.MaxBodySize = 1024 * 1024
	}
	if c.RPC.SSEBufferSize == 0 {
		c.RPC.SSEBufferSize = 64
	}
	if c.Consensus.BlockIntervalSec == 0 {
		c.Consensus.BlockIntervalSec = 2
	}
	if c.Consensus.MaxTimeDriftSec == 0 {
		c.Consensus.MaxTimeDriftSec = 15
	}
	if c.LogLevel == "" {
		c.LogLevel = DefaultLogLevel
	}
}

// DefaultConfig returns a single-node development configuration.
func DefaultConfig() *Config {
	cfg := &Config{
		NodeID:      "node0",
		DataDir:     "./data",
		RPCPort:     DefaultRPCPort,
		P2PPort:     DefaultP2PPort,
		MaxBlockTxs:     DefaultMaxBlockTxs,
		LogLevel:        DefaultLogLevel,
		PruneKeepBlocks: DefaultPruneKeepBlocks,
		Genesis: GenesisConfig{
			ChainID: "tolchain-dev",
			Alloc:   map[string]uint64{},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

// Load reads a JSON config file from path and validates required fields.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}
	return cfg, nil
}

// Validate checks that all required fields are present and well-formed.
func (c *Config) Validate() error {
	if c.NodeID == "" {
		return fmt.Errorf("node_id must not be empty")
	}
	if c.DataDir == "" {
		return fmt.Errorf("data_dir must not be empty")
	}
	if c.Genesis.ChainID == "" {
		return fmt.Errorf("genesis.chain_id must not be empty")
	}
	if c.RPCPort <= 0 || c.RPCPort > 65535 {
		return fmt.Errorf("rpc_port must be 1-65535, got %d", c.RPCPort)
	}
	if c.P2PPort <= 0 || c.P2PPort > 65535 {
		return fmt.Errorf("p2p_port must be 1-65535, got %d", c.P2PPort)
	}
	if c.RPCPort == c.P2PPort {
		return fmt.Errorf("rpc_port and p2p_port must not be the same (%d)", c.RPCPort)
	}
	if c.MaxBlockTxs > MaxBlockTxsUpperBound {
		return fmt.Errorf("max_block_txs must be at most %d, got %d", MaxBlockTxsUpperBound, c.MaxBlockTxs)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error", "":
		// valid
	default:
		return fmt.Errorf("log_level must be one of debug/info/warn/error, got %q", c.LogLevel)
	}
	if len(c.Validators) == 0 {
		return fmt.Errorf("validators list must not be empty")
	}
	seen := make(map[string]bool, len(c.Validators))
	for i, v := range c.Validators {
		b, err := hex.DecodeString(v)
		if err != nil || len(b) != 32 {
			return fmt.Errorf("validators[%d]: must be 64-char hex (32 bytes ed25519 pubkey), got %q", i, v)
		}
		if seen[v] {
			return fmt.Errorf("validators[%d]: duplicate pubkey %q", i, v)
		}
		seen[v] = true
	}
	if c.TLS != nil {
		t := c.TLS
		allSet := t.CACert != "" && t.NodeCert != "" && t.NodeKey != ""
		allEmpty := t.CACert == "" && t.NodeCert == "" && t.NodeKey == ""
		if !allSet && !allEmpty {
			return fmt.Errorf("tls: all three paths (ca_cert, node_cert, node_key) must be set or all empty")
		}
	}
	return nil
}

// Save writes the config to path as formatted JSON.
func Save(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
