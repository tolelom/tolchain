package config

import (
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// GenesisHash is a canonical all-zeros previous hash for the genesis block.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// CreateGenesisBlock builds and signs block #0 from the config's Alloc map.
// It also sets initial account balances in state and commits.
func CreateGenesisBlock(cfg *Config, state core.State, proposerPriv crypto.PrivateKey) (*core.Block, error) {
	proposerPub := proposerPriv.Public()

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

	stateRoot := state.ComputeRoot()
	if err := state.Commit(); err != nil {
		return nil, err
	}

	block := core.NewBlock(cfg.Genesis.ChainID, 0, GenesisHash, proposerPub.Hex(), nil)
	block.Header.StateRoot = stateRoot
	// Use the canonical empty TxRoot (genesis has no transactions).
	block.Header.TxRoot = core.ComputeTxRoot(nil)
	block.Sign(proposerPriv)
	return block, nil
}

// IsGenesisHash returns true if the hash is the canonical genesis prev-hash.
func IsGenesisHash(h string) bool {
	return h == GenesisHash
}
