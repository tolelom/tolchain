package config

import (
	"fmt"
	"math"

	"github.com/tolelom/tolchain/core"
)

// GenesisHash is the PrevHash of block 0.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// CreateGenesisBlock credits the alloc accounts, commits that state and
// returns block 0.
//
// Block 0 is derived only from GenesisConfig and has no proposer or
// signature, so every node builds the same hash. That is safe because it is
// never received from peers: sync starts at height 1.
func CreateGenesisBlock(cfg *Config, state core.State) (*core.Block, error) {
	var totalAlloc uint64
	for pubkeyHex, balance := range cfg.Genesis.Alloc {
		if err := state.SetAccount(&core.Account{Address: pubkeyHex, Balance: balance}); err != nil {
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

	if totalAlloc > 0 || cfg.MaxTotalSupply > 0 {
		if err := state.SetAccount(&core.Account{Address: core.TotalSupplyAddress, Balance: totalAlloc}); err != nil {
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

	// Not core.NewBlock: it stamps time.Now(), which differs per node.
	block := &core.Block{
		Header: core.BlockHeader{
			ChainID:   cfg.Genesis.ChainID,
			Height:    0,
			PrevHash:  GenesisHash,
			StateRoot: stateRoot,
			TxRoot:    core.ComputeTxRoot(nil),
			Timestamp: cfg.Genesis.Timestamp,
		},
	}
	block.Hash = block.ComputeHash()
	return block, nil
}

func IsGenesisHash(h string) bool {
	return h == GenesisHash
}
