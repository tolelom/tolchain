package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

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
	Genesis      GenesisConfig `json:"genesis"`
	SeedPeers    []SeedPeer    `json:"seed_peers,omitempty"`     // initial peers to connect to
	TLS          *TLSConfig    `json:"tls,omitempty"`           // nil → plain TCP
	RPCAuthToken string        `json:"rpc_auth_token,omitempty"` // empty → no auth
}

// DefaultConfig returns a single-node development configuration.
func DefaultConfig() *Config {
	return &Config{
		NodeID:      "node0",
		DataDir:     "./data",
		RPCPort:     8545,
		P2PPort:     30303,
		MaxBlockTxs: 500,
		Genesis: GenesisConfig{
			ChainID: "tolchain-dev",
			Alloc:   map[string]uint64{},
		},
	}
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
	if len(c.Validators) == 0 {
		return fmt.Errorf("validators list must not be empty")
	}
	for i, v := range c.Validators {
		b, err := hex.DecodeString(v)
		if err != nil || len(b) != 32 {
			return fmt.Errorf("validators[%d]: must be 64-char hex (32 bytes ed25519 pubkey), got %q", i, v)
		}
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
