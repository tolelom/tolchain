// Package consensus implements Proof-of-Authority block production.
// Validators propose blocks in round-robin order. Each block is signed by
// the proposer; other nodes verify the signature before accepting the block.
package consensus

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/metrics"
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
	idx := int(nextHeight % int64(len(p.cfg.Validators)))
	return p.cfg.Validators[idx] == p.pubKey.Hex()
}

// ProduceBlock builds, signs, executes and commits the next block.
// Failed transactions are skipped (and removed from the mempool) so that a
// single invalid tx cannot stall block production.
//
// Trust Model: PoA assumes validators are trusted entities. A malicious validator
// can censor transactions by excluding them from blocks. This is an accepted
// trade-off for the simplicity and performance of round-robin PoA consensus.
// For production deployments with untrusted validators, consider adding a
// transaction inclusion watchdog or switching to a BFT consensus protocol.
func (p *PoA) ProduceBlock() (*core.Block, error) {
	start := time.Now()
	if !p.IsProposer() {
		return nil, errors.New("not the proposer for this round")
	}

	limit := p.cfg.MaxBlockTxs
	if limit <= 0 {
		limit = defaultMaxBlockTxs
	}
	candidates := p.mempool.Pending(limit)

	// Acquire the block-execution write lock so that RPC queries (which hold
	// the read lock) cannot observe partially-applied state.
	p.state.BlockLock()

	// Read the tip only after acquiring the lock so the produced block cannot
	// be based on a stale tip (e.g. a block accepted by sync in between).
	tip := p.bc.Tip()
	var prevHash string
	var nextHeight int64
	// Genesis block (height 0) is created by CreateGenesisBlock in main.go,
	// not by ProduceBlock. If tip is nil here, it means genesis is missing,
	// and the produced block at height 1 will be rejected by AddBlock.
	if tip == nil {
		prevHash = config.GenesisHash
		nextHeight = 1
	} else {
		prevHash = tip.Hash
		nextHeight = tip.Header.Height + 1
	}

	// Take a block-level snapshot so we can rollback all state changes if
	// AddBlock fails later.
	blockSnapID, err := p.state.Snapshot()
	if err != nil {
		p.state.BlockUnlock()
		return nil, fmt.Errorf("block snapshot: %w", err)
	}

	// Buffer events during execution so they are only delivered after commit.
	buf := events.NewBuffer()
	p.exec.SetEmitter(buf)

	// Build a temporary block to provide context during execution.
	tmpBlock := core.NewBlock(p.cfg.Genesis.ChainID, nextHeight, prevHash, p.pubKey.Hex(), candidates)

	// Execute each transaction individually; keep only successful ones.
	var successTxs []*core.Transaction
	var failedIDs []string
	for _, tx := range candidates {
		if txErr := p.exec.ExecuteTx(tmpBlock, tx); txErr != nil {
			slog.Warn("skipping failed tx", "component", "consensus", "tx_id", tx.ID, "error", txErr)
			metrics.TxFailed.Inc()
			failedIDs = append(failedIDs, tx.ID)
			continue
		}
		successTxs = append(successTxs, tx)
	}

	// Restore the real emitter.
	p.exec.SetEmitter(p.emitter)

	// Reuse tmpBlock as the final block so the broadcast header carries the
	// exact timestamp the handlers observed during execution (handlers store
	// it into state, e.g. Asset.MintedAt). Creating a second block here would
	// stamp a fresh time.Now() and make syncing nodes compute a different
	// StateRoot when they re-execute the block.
	block := tmpBlock
	block.Transactions = successTxs
	block.Header.TxRoot = core.ComputeTxRoot(successTxs)

	// Compute root from the write buffer BEFORE flushing so that if AddBlock
	// fails the state has not yet been persisted and the node stays consistent.
	stateRoot, err := p.state.ComputeRoot()
	if err != nil {
		buf.Discard()
		if revErr := p.state.RevertToSnapshot(blockSnapID); revErr != nil {
			slog.Error("FATAL: state root computation failed and snapshot revert failed",
				"component", "consensus", "height", block.Header.Height, "revert_error", revErr, "root_error", err)
			os.Exit(1)
		}
		p.state.BlockUnlock()
		return nil, fmt.Errorf("compute state root: %w", err)
	}
	block.Header.StateRoot = stateRoot
	block.Sign(p.privKey)

	// Defensive self-validation: run the same checks peers will run before
	// broadcasting. Catches proposer bugs (wrong index, timestamp drift, hash
	// mismatch) early instead of letting peers reject the broadcast.
	if err := p.ValidateBlock(block); err != nil {
		buf.Discard()
		if revErr := p.state.RevertToSnapshot(blockSnapID); revErr != nil {
			slog.Error("FATAL: self-validation failed and snapshot revert failed",
				"component", "consensus", "height", block.Header.Height, "revert_error", revErr, "validation_error", err)
			os.Exit(1)
		}
		p.state.BlockUnlock()
		return nil, fmt.Errorf("self-validation: %w", err)
	}

	if err := p.bc.AddBlock(block); err != nil {
		// Discard buffered events and rollback all state changes.
		buf.Discard()
		if revErr := p.state.RevertToSnapshot(blockSnapID); revErr != nil {
			slog.Error("FATAL: AddBlock failed and snapshot revert failed", "component", "consensus", "height", block.Header.Height, "revert_error", revErr, "add_error", err)
			os.Exit(1)
		}
		p.state.BlockUnlock()
		return nil, fmt.Errorf("add block: %w", err)
	}

	// Flush state only after the block is safely stored.
	if err := p.state.Commit(); err != nil {
		slog.Error("FATAL: block stored but state commit failed", "component", "consensus", "height", block.Header.Height, "error", err)
		os.Exit(1)
	}

	// Release the block-execution lock now that state is fully committed.
	p.state.BlockUnlock()

	// Flush buffered tx-level events now that the block is committed.
	buf.Flush(p.emitter)

	// Emit block commit after Sign() so block.Hash is set correctly.
	p.emitter.Emit(events.Event{
		Type:        events.EventBlockCommit,
		BlockHeight: block.Header.Height,
		Data:        map[string]any{"hash": block.Hash, "txs": len(block.Transactions), "block": block},
	})

	txIDs := make([]string, len(successTxs))
	for i, tx := range successTxs {
		txIDs[i] = tx.ID
	}
	p.mempool.Remove(txIDs)

	// Remove failed transactions only after the block is committed. Removing
	// them earlier would permanently drop txs that failed solely because block
	// production was aborted (stale tip, AddBlock failure, ...).
	if len(failedIDs) > 0 {
		p.mempool.Remove(failedIDs)
	}

	metrics.BlocksProduced.Inc()
	metrics.BlockHeight.Set(float64(block.Header.Height))
	metrics.BlockProductionDuration.Observe(time.Since(start).Seconds())
	metrics.BlockTxCount.Observe(float64(len(successTxs)))

	return block, nil
}

const (
	// defaultMaxBlockTxs is the fallback limit when Config.MaxBlockTxs is not set.
	defaultMaxBlockTxs = 500
)

// ValidateBlock checks that block was proposed by the expected validator.
func (p *PoA) ValidateBlock(block *core.Block) error {
	if len(p.cfg.Validators) == 0 {
		return errors.New("no validators configured")
	}

	// (B) Chain ID must match this node's network.
	if block.Header.ChainID != p.cfg.Genesis.ChainID {
		return fmt.Errorf("chain ID mismatch: got %q want %q", block.Header.ChainID, p.cfg.Genesis.ChainID)
	}

	idx := int(block.Header.Height % int64(len(p.cfg.Validators)))
	expected := p.cfg.Validators[idx]
	if block.Header.Proposer != expected {
		return fmt.Errorf("wrong proposer: got %s want %s", block.Header.Proposer, expected)
	}

	pub, err := crypto.PubKeyFromHex(block.Header.Proposer)
	if err != nil {
		return fmt.Errorf("invalid proposer pubkey: %w", err)
	}
	// Verify() re-computes the header hash and checks the signature,
	// preventing acceptance of blocks with a tampered header.
	if err := block.Verify(pub); err != nil {
		return fmt.Errorf("block signature invalid: %w", err)
	}
	// Independently verify TxRoot matches the actual transaction list.
	if txRoot := core.ComputeTxRoot(block.Transactions); block.Header.TxRoot != txRoot {
		return fmt.Errorf("tx_root mismatch: got %s want %s", block.Header.TxRoot, txRoot)
	}

	// Block timestamp must be positive (non-zero, non-negative).
	if block.Header.Timestamp <= 0 {
		return fmt.Errorf("invalid block timestamp: %d", block.Header.Timestamp)
	}

	// (C) Timestamp validation: must not be too far in the future
	// and must be >= the previous block's timestamp.
	now := time.Now().UnixNano()
	maxTimeDrift := int64(p.cfg.Consensus.MaxTimeDriftSec) * int64(time.Second)
	if block.Header.Timestamp > now+maxTimeDrift {
		return fmt.Errorf("block timestamp too far in future: %d (now %d)", block.Header.Timestamp, now)
	}
	// Hard cap: reject blocks more than 1 hour in the future regardless of config.
	const maxFutureDrift = int64(time.Hour)
	if block.Header.Timestamp > now+maxFutureDrift {
		return fmt.Errorf("block timestamp exceeds 1h hard cap: %d (now %d)", block.Header.Timestamp, now)
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
		// Timestamp must not go backwards.
		if block.Header.Timestamp < tip.Header.Timestamp {
			return fmt.Errorf("block timestamp %d < previous block %d", block.Header.Timestamp, tip.Header.Timestamp)
		}
	}
	return nil
}

// Run starts the block-production loop with the given interval. It blocks
// until done is closed.
//
// Every Nth tick, expired mempool transactions are evicted regardless of whether
// this node is the proposer — otherwise non-proposer nodes accumulate dead
// entries that never get cleaned up by ProduceBlock's per-tx skip.
func (p *PoA) Run(interval time.Duration, done <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	const evictEvery = 30 // ticks between mempool sweeps (≈60s at 2s interval)
	tick := 0
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			tick++
			if tick%evictEvery == 0 && p.mempool != nil {
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
