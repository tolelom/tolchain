// Package consensus implements Proof-of-Authority block production.
// Validators propose blocks in round-robin order. Each block is signed by
// the proposer; other nodes verify the signature before accepting the block.
package consensus

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

// PoA is the Proof-of-Authority consensus engine.
type PoA struct {
	cfg     *config.Config
	bc      *core.Blockchain
	state   core.State
	mempool *core.Mempool
	exec    *vm.Executor
	emitter *events.Emitter
	privKey crypto.PrivateKey
	pubKey  crypto.PublicKey
}

// New creates a PoA engine for the local validator identified by privKey.
func New(
	cfg *config.Config,
	bc *core.Blockchain,
	state core.State,
	mempool *core.Mempool,
	exec *vm.Executor,
	emitter *events.Emitter,
	privKey crypto.PrivateKey,
) *PoA {
	return &PoA{
		cfg:     cfg,
		bc:      bc,
		state:   state,
		mempool: mempool,
		exec:    exec,
		emitter: emitter,
		privKey: privKey,
		pubKey:  privKey.Public(),
	}
}

// IsProposer reports whether this node should propose the next block.
func (p *PoA) IsProposer() bool {
	if len(p.cfg.Validators) == 0 {
		return false
	}
	nextHeight := p.bc.Height() + 1
	idx := int(nextHeight) % len(p.cfg.Validators)
	return p.cfg.Validators[idx] == p.pubKey.Hex()
}

// ProduceBlock builds, signs, executes and commits the next block.
func (p *PoA) ProduceBlock() (*core.Block, error) {
	if !p.IsProposer() {
		return nil, errors.New("not the proposer for this round")
	}

	limit := p.cfg.MaxBlockTxs
	if limit <= 0 {
		limit = 500
	}
	txs := p.mempool.Pending(limit)

	tip := p.bc.Tip()
	var prevHash string
	var nextHeight int64
	if tip == nil {
		prevHash = config.GenesisHash
		nextHeight = 1
	} else {
		prevHash = tip.Hash
		nextHeight = tip.Header.Height + 1
	}

	block := core.NewBlock(nextHeight, prevHash, p.pubKey.Hex(), txs)

	if err := p.exec.ExecuteBlock(block); err != nil {
		return nil, fmt.Errorf("execute block: %w", err)
	}

	// Compute root from the write buffer BEFORE flushing so that if AddBlock
	// fails the state has not yet been persisted and the node stays consistent.
	block.Header.StateRoot = p.state.ComputeRoot()
	block.Sign(p.privKey)

	if err := p.bc.AddBlock(block); err != nil {
		return nil, fmt.Errorf("add block: %w", err)
	}

	// Flush state only after the block is safely stored.
	if err := p.state.Commit(); err != nil {
		log.Fatalf("[consensus] FATAL: block %d stored but state commit failed: %v",
			block.Header.Height, err)
	}

	// Emit after Sign() so block.Hash is set correctly.
	p.emitter.Emit(events.Event{
		Type:        events.EventBlockCommit,
		BlockHeight: block.Header.Height,
		Data:        map[string]any{"hash": block.Hash, "txs": len(block.Transactions)},
	})

	txIDs := make([]string, len(txs))
	for i, tx := range txs {
		txIDs[i] = tx.ID
	}
	p.mempool.Remove(txIDs)

	return block, nil
}

// ValidateBlock checks that block was proposed by the expected validator.
func (p *PoA) ValidateBlock(block *core.Block) error {
	if len(p.cfg.Validators) == 0 {
		return errors.New("no validators configured")
	}
	idx := int(block.Header.Height) % len(p.cfg.Validators)
	expected := p.cfg.Validators[idx]
	if block.Header.Proposer != expected {
		return fmt.Errorf("wrong proposer: got %s want %s", block.Header.Proposer, expected)
	}

	pub, err := crypto.PubKeyFromHex(block.Header.Proposer)
	if err != nil {
		return fmt.Errorf("invalid proposer pubkey: %w", err)
	}
	if err := block.Verify(pub); err != nil {
		return fmt.Errorf("block signature invalid: %w", err)
	}

	// Validate previous hash linkage
	tip := p.bc.Tip()
	if tip == nil {
		if !config.IsGenesisHash(block.Header.PrevHash) {
			return errors.New("first block must reference genesis prev-hash")
		}
	} else {
		if block.Header.PrevHash != tip.Hash {
			return fmt.Errorf("prev_hash mismatch: got %s want %s", block.Header.PrevHash, tip.Hash)
		}
		if block.Header.Height != tip.Header.Height+1 {
			return fmt.Errorf("height mismatch: got %d want %d", block.Header.Height, tip.Header.Height+1)
		}
	}
	return nil
}

// Run starts the block-production loop with the given interval. It blocks
// until done is closed.
func (p *PoA) Run(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if p.IsProposer() {
				if _, err := p.ProduceBlock(); err != nil {
					log.Printf("[consensus] produce block error: %v", err)
				}
			}
		}
	}
}
