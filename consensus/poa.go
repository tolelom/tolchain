// Package consensus is round-robin Proof-of-Authority: the validator at
// height % len(validators) proposes and signs each block.
package consensus

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/vm"
)

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

// IsProposer reports whether this node proposes the next block.
func (p *PoA) IsProposer() bool {
	if len(p.cfg.Validators) == 0 {
		return false
	}
	return p.proposerAt(p.bc.Height()+1) == p.pubKey.Hex()
}

func (p *PoA) proposerAt(height int64) string {
	return p.cfg.Validators[int(height%int64(len(p.cfg.Validators)))]
}

// ProduceBlock builds, executes, signs and commits the next block, skipping
// txs that fail.
//
// Validators are trusted: one can censor txs by leaving them out. That is the
// accepted price of round-robin PoA; untrusted validators would need BFT.
func (p *PoA) ProduceBlock() (*core.Block, error) {
	s := &produceBlock{p: p, start: time.Now()}
	if err := RunProduceBlock(s); err != nil {
		var fe *FlowError
		if errors.As(err, &fe) {
			return nil, fe.Err
		}
		return nil, err
	}
	return s.block, nil
}

// maxFutureDrift bounds block timestamps regardless of the configured drift.
const maxFutureDrift = int64(time.Hour)

// ValidateBlock checks a received block against the chain tip: chain, the
// round-robin proposer, its signature, TxRoot, timestamp and linkage.
func (p *PoA) ValidateBlock(block *core.Block) error {
	if len(p.cfg.Validators) == 0 {
		return errors.New("no validators configured")
	}
	if block.Header.ChainID != p.cfg.Genesis.ChainID {
		return fmt.Errorf("chain ID mismatch: got %q want %q", block.Header.ChainID, p.cfg.Genesis.ChainID)
	}
	if err := p.checkProposerSignature(block); err != nil {
		return err
	}
	if txRoot := core.ComputeTxRoot(block.Transactions); block.Header.TxRoot != txRoot {
		return fmt.Errorf("tx_root mismatch: got %s want %s", block.Header.TxRoot, txRoot)
	}
	if err := p.checkTimestamp(block); err != nil {
		return err
	}
	return p.checkLinkage(block)
}

func (p *PoA) checkProposerSignature(block *core.Block) error {
	if expected := p.proposerAt(block.Header.Height); block.Header.Proposer != expected {
		return fmt.Errorf("wrong proposer: got %s want %s", block.Header.Proposer, expected)
	}
	pub, err := crypto.PubKeyFromHex(block.Header.Proposer)
	if err != nil {
		return fmt.Errorf("invalid proposer pubkey: %w", err)
	}
	if err := block.Verify(pub); err != nil {
		return fmt.Errorf("block signature invalid: %w", err)
	}
	return nil
}

// checkTimestamp bounds only the future, by the local clock; checkLinkage
// keeps timestamps from going backwards.
func (p *PoA) checkTimestamp(block *core.Block) error {
	if block.Header.Timestamp <= 0 {
		return fmt.Errorf("invalid block timestamp: %d", block.Header.Timestamp)
	}
	now := time.Now().UnixNano()
	maxTimeDrift := int64(p.cfg.Consensus.MaxTimeDriftSec) * int64(time.Second)
	if block.Header.Timestamp > now+maxTimeDrift {
		return fmt.Errorf("block timestamp too far in future: %d (now %d)", block.Header.Timestamp, now)
	}
	if block.Header.Timestamp > now+maxFutureDrift {
		return fmt.Errorf("block timestamp exceeds 1h hard cap: %d (now %d)", block.Header.Timestamp, now)
	}
	return nil
}

func (p *PoA) checkLinkage(block *core.Block) error {
	tip := p.bc.Tip()
	if tip == nil {
		if !config.IsGenesisHash(block.Header.PrevHash) {
			return errors.New("first block must reference genesis prev-hash")
		}
		return nil
	}
	if block.Header.PrevHash != tip.Hash {
		return fmt.Errorf("prev_hash mismatch: got %s want %s", block.Header.PrevHash, tip.Hash)
	}
	if block.Header.Height != tip.Header.Height+1 {
		return fmt.Errorf("height mismatch: got %d want %d", block.Header.Height, tip.Header.Height+1)
	}
	if block.Header.Timestamp < tip.Header.Timestamp {
		return fmt.Errorf("block timestamp %d < previous block %d", block.Header.Timestamp, tip.Header.Timestamp)
	}
	return nil
}

// mempoolEvictEvery is in ticks (about a minute at the default 2s interval).
const mempoolEvictEvery = 30

// Run produces a block on each tick where this node is the proposer, until
// done is closed. Every node also evicts expired mempool txs periodically:
// non-proposers would otherwise never drop them.
func (p *PoA) Run(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	tick := 0
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			tick++
			if tick%mempoolEvictEvery == 0 && p.mempool != nil {
				if n := p.mempool.EvictExpired(); n > 0 {
					slog.Info("mempool expired txs evicted", "component", "consensus", "count", n)
				}
			}
			if p.IsProposer() {
				if _, err := p.ProduceBlock(); err != nil {
					slog.Error("produce block error", "component", "consensus", "error", err)
				}
			}
		}
	}
}
