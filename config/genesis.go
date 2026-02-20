package config

import (
	"strings"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
)

// GenesisHash is a canonical all-zeros previous hash for the genesis block.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// CreateGenesisBlock builds and signs block #0 from the config's Alloc map.
// It also sets initial account balances in state and commits.
func CreateGenesisBlock(cfg *Config, state core.State, proposerPriv crypto.PrivateKey) (*core.Block, error) {
	proposerPub := proposerPriv.Public()

	// Credit all alloc accounts
	for pubkeyHex, balance := range cfg.Genesis.Alloc {
		acc := &core.Account{
			Address: pubkeyHex,
			Balance: balance,
			Nonce:   0,
		}
		if err := state.SetAccount(acc); err != nil {
			return nil, err
		}
	}

	stateRoot := state.ComputeRoot()
	if err := state.Commit(); err != nil {
		return nil, err
	}

	block := core.NewBlock(0, GenesisHash, proposerPub.Hex(), nil)
	block.Header.StateRoot = stateRoot
	// Embed chain ID in PrevHash comment via TxRoot for identification
	block.Header.TxRoot = crypto.Hash([]byte(cfg.Genesis.ChainID))
	block.Sign(proposerPriv)
	return block, nil
}

// IsGenesisHash returns true if the hash is the canonical genesis prev-hash.
func IsGenesisHash(h string) bool {
	return strings.Count(h, "0") == len(h) && len(h) == 64
}
