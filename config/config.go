package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/tolelom/tolchain/core"
)

const (
	DefaultRPCPort        = 8545
	DefaultP2PPort        = 30303
	DefaultMaxBlockTxs    = 500
	MaxBlockTxsUpperBound = 10000
	DefaultLogLevel       = "info"
	// MinPruneKeepBlocks is the smallest non-zero prune_keep_blocks. A random
	// reveal reads a block hash up to this far back; if the producer had pruned
	// it, it would reject reveals that other nodes accept and the chain forks.
	MinPruneKeepBlocks = core.RandomTargetDelay + core.RandomRevealWindow
	// DefaultPruneKeepBlocks of 0 keeps every block.
	DefaultPruneKeepBlocks = 0
	// DefaultGenesisTimestamp (2025-01-01T00:00:00Z in Unix ns) keeps the
	// genesis hash identical across nodes that omit genesis.timestamp.
	DefaultGenesisTimestamp int64 = 1735689600000000000
)

// Zero-valued fields of the sections below are filled by ApplyDefaults.

type MempoolConfig struct {
	MaxSize      int `json:"max_size,omitempty"`
	MaxTxAgeSec  int `json:"max_tx_age_sec,omitempty"`
	MaxFutureSec int `json:"max_future_sec,omitempty"`
}

type NetworkConfig struct {
	MaxPeers         int `json:"max_peers,omitempty"`
	WriteTimeoutSec  int `json:"write_timeout_sec,omitempty"`
	ReadTimeoutSec   int `json:"read_timeout_sec,omitempty"`
	MaxMessageSize   int `json:"max_message_size,omitempty"` // bytes
	SyncBatchSize    int `json:"sync_batch_size,omitempty"`
	MaxSyncBatchSize int `json:"max_sync_batch_size,omitempty"`
}

type RPCServerConfig struct {
	RateLimit       float64 `json:"rate_limit,omitempty"` // requests/sec per client IP
	RateBurst       int     `json:"rate_burst,omitempty"`
	ReadTimeoutSec  int     `json:"read_timeout_sec,omitempty"`
	WriteTimeoutSec int     `json:"write_timeout_sec,omitempty"`
	IdleTimeoutSec  int     `json:"idle_timeout_sec,omitempty"`
	MaxBodySize     int64   `json:"max_body_size,omitempty"` // bytes
	SSEBufferSize   int     `json:"sse_buffer_size,omitempty"`
	// TrustedProxies lists IPs/CIDRs whose X-Forwarded-For is believed for
	// rate limiting. Only the rightmost hop is used: it is the one the proxy
	// appended, everything left of it is client-controlled. Leave empty when
	// the node is exposed directly.
	TrustedProxies []string `json:"trusted_proxies,omitempty"`
}

type ConsensusConfig struct {
	BlockIntervalSec int `json:"block_interval_sec,omitempty"`
	MaxTimeDriftSec  int `json:"max_time_drift_sec,omitempty"`
}

// TLSConfig holds PEM paths for P2P mTLS; all empty means plain TCP.
type TLSConfig struct {
	CACert   string `json:"ca_cert"`
	NodeCert string `json:"node_cert"`
	NodeKey  string `json:"node_key"`
}

type SeedPeer struct {
	ID   string `json:"id"`
	Addr string `json:"addr"` // host:port
}

// GenesisConfig must be identical on every node: the genesis block is derived
// from it, so any difference forks the chain at height 1.
type GenesisConfig struct {
	ChainID string            `json:"chain_id"`
	Alloc   map[string]uint64 `json:"alloc"` // pubkey hex → initial balance
	// Timestamp in Unix ns. It must not be in the future, because every later
	// block must carry a timestamp at or after it.
	Timestamp int64 `json:"timestamp,omitempty"`
}

type Config struct {
	NodeID      string        `json:"node_id"`
	DataDir     string        `json:"data_dir"`
	RPCPort     int           `json:"rpc_port"`
	P2PPort     int           `json:"p2p_port"`
	MaxBlockTxs int           `json:"max_block_txs"`       // 0 → DefaultMaxBlockTxs
	Validators  []string      `json:"validators"`          // proposer pubkeys, in round-robin order
	Operators   []string      `json:"operators,omitempty"` // empty → every sender may run operator txs
	Genesis     GenesisConfig `json:"genesis"`
	SeedPeers   []SeedPeer    `json:"seed_peers,omitempty"`
	TLS         *TLSConfig    `json:"tls,omitempty"`
	// RPCAuthToken empty → RPC needs no auth.
	RPCAuthToken    string          `json:"rpc_auth_token,omitempty"`
	LogLevel        string          `json:"log_level,omitempty"`
	PruneKeepBlocks int64           `json:"prune_keep_blocks,omitempty"`
	MaxTotalSupply  uint64          `json:"max_total_supply,omitempty"` // 0 → unlimited
	Mempool         MempoolConfig   `json:"mempool,omitempty"`
	Network         NetworkConfig   `json:"network,omitempty"`
	RPC             RPCServerConfig `json:"rpc,omitempty"`
	Consensus       ConsensusConfig `json:"consensus,omitempty"`
}

func setDefault[T comparable](field *T, value T) {
	var zero T
	if *field == zero {
		*field = value
	}
}

// ApplyDefaults fills zero-valued tunables.
func (c *Config) ApplyDefaults() {
	setDefault(&c.Mempool.MaxSize, 10000)
	setDefault(&c.Mempool.MaxTxAgeSec, 3600)
	setDefault(&c.Mempool.MaxFutureSec, 300)

	setDefault(&c.Network.MaxPeers, 50)
	setDefault(&c.Network.WriteTimeoutSec, 30)
	setDefault(&c.Network.ReadTimeoutSec, 300)
	setDefault(&c.Network.MaxMessageSize, 10*1024*1024)
	setDefault(&c.Network.SyncBatchSize, 50)
	setDefault(&c.Network.MaxSyncBatchSize, 200)

	setDefault(&c.RPC.RateLimit, 100)
	setDefault(&c.RPC.RateBurst, 200)
	setDefault(&c.RPC.ReadTimeoutSec, 30)
	setDefault(&c.RPC.WriteTimeoutSec, 30)
	setDefault(&c.RPC.IdleTimeoutSec, 60)
	setDefault(&c.RPC.MaxBodySize, 1024*1024)
	setDefault(&c.RPC.SSEBufferSize, 64)

	setDefault(&c.Consensus.BlockIntervalSec, 2)
	setDefault(&c.Consensus.MaxTimeDriftSec, 15)

	setDefault(&c.LogLevel, DefaultLogLevel)
	setDefault(&c.Genesis.Timestamp, DefaultGenesisTimestamp)
}

// DefaultConfig is the single-node development configuration.
func DefaultConfig() *Config {
	cfg := &Config{
		NodeID:          "node0",
		DataDir:         "./data",
		RPCPort:         DefaultRPCPort,
		P2PPort:         DefaultP2PPort,
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

// Validate rejects missing, malformed or contradictory settings.
func (c *Config) Validate() error {
	if c.NodeID == "" {
		return fmt.Errorf("node_id must not be empty")
	}
	if c.DataDir == "" {
		return fmt.Errorf("data_dir must not be empty")
	}
	for _, check := range []func() error{
		c.validateGenesis,
		c.validatePorts,
		c.validateLimits,
		c.validateNonNegative,
		c.validateLogLevel,
		c.validateKeys,
		c.validateTrustedProxies,
		c.validateTLS,
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateGenesis() error {
	if c.Genesis.ChainID == "" {
		return fmt.Errorf("genesis.chain_id must not be empty")
	}
	if c.Genesis.Timestamp < 0 {
		return fmt.Errorf("genesis.timestamp must not be negative, got %d", c.Genesis.Timestamp)
	}
	// Same 1h bound as the consensus future-timestamp cap; beyond it the value
	// is most likely in the wrong unit.
	if maxTs := time.Now().Add(time.Hour).UnixNano(); c.Genesis.Timestamp > maxTs {
		return fmt.Errorf("genesis.timestamp %d is more than 1h in the future (unix nanoseconds expected)", c.Genesis.Timestamp)
	}
	return nil
}

func (c *Config) validatePorts() error {
	if c.RPCPort <= 0 || c.RPCPort > 65535 {
		return fmt.Errorf("rpc_port must be 1-65535, got %d", c.RPCPort)
	}
	if c.P2PPort <= 0 || c.P2PPort > 65535 {
		return fmt.Errorf("p2p_port must be 1-65535, got %d", c.P2PPort)
	}
	if c.RPCPort == c.P2PPort {
		return fmt.Errorf("rpc_port and p2p_port must not be the same (%d)", c.RPCPort)
	}
	return nil
}

func (c *Config) validateLimits() error {
	if c.MaxBlockTxs > MaxBlockTxsUpperBound {
		return fmt.Errorf("max_block_txs must be at most %d, got %d", MaxBlockTxsUpperBound, c.MaxBlockTxs)
	}
	if c.PruneKeepBlocks < 0 || (c.PruneKeepBlocks > 0 && c.PruneKeepBlocks < MinPruneKeepBlocks) {
		return fmt.Errorf("prune_keep_blocks must be 0 (no pruning) or at least %d, got %d", MinPruneKeepBlocks, c.PruneKeepBlocks)
	}
	return nil
}

func (c *Config) validateLogLevel() error {
	switch c.LogLevel {
	case "debug", "info", "warn", "error", "":
		return nil
	}
	return fmt.Errorf("log_level must be one of debug/info/warn/error, got %q", c.LogLevel)
}

func isPubKeyHex(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}

func (c *Config) validateKeys() error {
	if len(c.Validators) == 0 {
		return fmt.Errorf("validators list must not be empty")
	}
	seen := make(map[string]bool, len(c.Validators))
	for i, v := range c.Validators {
		if !isPubKeyHex(v) {
			return fmt.Errorf("validators[%d]: must be 64-char hex (32 bytes ed25519 pubkey), got %q", i, v)
		}
		if seen[v] {
			return fmt.Errorf("validators[%d]: duplicate pubkey %q", i, v)
		}
		seen[v] = true
	}
	for i, o := range c.Operators {
		if !isPubKeyHex(o) {
			return fmt.Errorf("operators[%d]: must be 64-char hex (32 bytes ed25519 pubkey), got %q", i, o)
		}
	}
	return nil
}

func (c *Config) validateTrustedProxies() error {
	for i, e := range c.RPC.TrustedProxies {
		if strings.Contains(e, "/") {
			if _, _, err := net.ParseCIDR(e); err != nil {
				return fmt.Errorf("rpc.trusted_proxies[%d]: invalid CIDR %q", i, e)
			}
		} else if net.ParseIP(e) == nil {
			return fmt.Errorf("rpc.trusted_proxies[%d]: invalid IP %q", i, e)
		}
	}
	return nil
}

func (c *Config) validateTLS() error {
	if c.TLS == nil {
		return nil
	}
	t := c.TLS
	allSet := t.CACert != "" && t.NodeCert != "" && t.NodeKey != ""
	allEmpty := t.CACert == "" && t.NodeCert == "" && t.NodeKey == ""
	if !allSet && !allEmpty {
		return fmt.Errorf("tls: all three paths (ca_cert, node_cert, node_key) must be set or all empty")
	}
	return nil
}

func (c *Config) validateNonNegative() error {
	fields := []struct {
		name  string
		value float64
	}{
		{"max_block_txs", float64(c.MaxBlockTxs)},
		{"mempool.max_size", float64(c.Mempool.MaxSize)},
		{"mempool.max_tx_age_sec", float64(c.Mempool.MaxTxAgeSec)},
		{"mempool.max_future_sec", float64(c.Mempool.MaxFutureSec)},
		{"network.max_peers", float64(c.Network.MaxPeers)},
		{"network.write_timeout_sec", float64(c.Network.WriteTimeoutSec)},
		{"network.read_timeout_sec", float64(c.Network.ReadTimeoutSec)},
		{"network.max_message_size", float64(c.Network.MaxMessageSize)},
		{"network.sync_batch_size", float64(c.Network.SyncBatchSize)},
		{"network.max_sync_batch_size", float64(c.Network.MaxSyncBatchSize)},
		{"rpc.rate_limit", c.RPC.RateLimit},
		{"rpc.rate_burst", float64(c.RPC.RateBurst)},
		{"rpc.read_timeout_sec", float64(c.RPC.ReadTimeoutSec)},
		{"rpc.write_timeout_sec", float64(c.RPC.WriteTimeoutSec)},
		{"rpc.idle_timeout_sec", float64(c.RPC.IdleTimeoutSec)},
		{"rpc.max_body_size", float64(c.RPC.MaxBodySize)},
		{"rpc.sse_buffer_size", float64(c.RPC.SSEBufferSize)},
		{"consensus.block_interval_sec", float64(c.Consensus.BlockIntervalSec)},
		{"consensus.max_time_drift_sec", float64(c.Consensus.MaxTimeDriftSec)},
	}
	for _, f := range fields {
		if f.value < 0 {
			return fmt.Errorf("%s must not be negative, got %v", f.name, f.value)
		}
	}
	return nil
}

// Save writes with 0600: the config may hold the RPC auth token.
func Save(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
