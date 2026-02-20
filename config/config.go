package config

import (
	"encoding/json"
	"os"
)

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
	Validators  []string      `json:"validators"`    // authorised proposer pubkey hexes
	Genesis     GenesisConfig `json:"genesis"`
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

// Load reads a JSON config file from path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the config to path as formatted JSON.
func Save(cfg *Config, path string) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
