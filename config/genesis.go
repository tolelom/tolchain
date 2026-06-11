package config

import (
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
)

// GenesisHash is a canonical all-zeros previous hash for the genesis block.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// CreateGenesisBlock builds block #0 deterministically from the config.
// It also sets initial account balances in state and commits.
//
// The genesis block is fully derived from GenesisConfig: its timestamp comes
// from genesis.timestamp, and it carries no proposer and no signature. Every
// node bootstrapping from the same config therefore computes a byte-identical
// genesis hash, so multi-node fresh bootstraps cannot fork at block 1.
// Genesis is never validated through PoA ValidateBlock (it is built locally,
// never received from peers — sync always starts at height 1), so the missing
// signature does not weaken block validation.
func CreateGenesisBlock(cfg *Config, state core.State) (*core.Block, error) {
	// Credit all alloc accounts and track total supply.
	var totalAlloc uint64
	for pubkeyHex, balance := range cfg.Genesis.Alloc {
		acc := &core.Account{
			Address: pubkeyHex,
			Balance: balance,
			Nonce:   0,
		}
		if err := state.SetAccount(acc); err != nil {
			return nil, err
		}
		if totalAlloc > math.MaxUint64-balance {
			return nil, fmt.Errorf("genesis alloc overflow: total would exceed uint64 max")
		}
		totalAlloc += balance
	}

	if cfg.MaxTotalSupply > 0 && totalAlloc > cfg.MaxTotalSupply {
		return nil, fmt.Errorf("genesis alloc %d exceeds max_total_supply %d", totalAlloc, cfg.MaxTotalSupply)
	}

	// Initialize the total supply tracker so the cap check is accurate.
	if totalAlloc > 0 || cfg.MaxTotalSupply > 0 {
		supply := &core.Account{
			Address: "system:total_supply",
			Balance: totalAlloc,
			Nonce:   0,
		}
		if err := state.SetAccount(supply); err != nil {
			return nil, err
		}
	}

	stateRoot, err := state.ComputeRoot()
	if err != nil {
		return nil, fmt.Errorf("compute genesis state root: %w", err)
	}
	if err := state.Commit(); err != nil {
		return nil, err
	}

	// Build the header by hand instead of core.NewBlock: NewBlock stamps
	// time.Now(), which would make the genesis hash differ per node.
	block := &core.Block{
		Header: core.BlockHeader{
			ChainID:   cfg.Genesis.ChainID,
			Height:    0,
			PrevHash:  GenesisHash,
			StateRoot: stateRoot,
			// Canonical empty TxRoot (genesis has no transactions).
			TxRoot:    core.ComputeTxRoot(nil),
			Timestamp: cfg.Genesis.Timestamp,
			Proposer:  "", // deterministic genesis: no proposer, no signature
		},
	}
	block.Hash = block.ComputeHash()
	return block, nil
}

// IsGenesisHash returns true if the hash is the canonical genesis prev-hash.
func IsGenesisHash(h string) bool {
	return h == GenesisHash
}
