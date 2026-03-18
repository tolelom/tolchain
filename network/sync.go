package network

import (
	"encoding/json"
	"log/slog"
	"os"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
)


// GetBlocksRequest asks a peer for blocks starting at FromHeight.
type GetBlocksRequest struct {
	FromHeight int64 `json:"from_height"`
	Limit      int   `json:"limit"`
}

// BlocksResponse carries a batch of blocks.
type BlocksResponse struct {
	Blocks []*core.Block `json:"blocks"`
}

// BlockValidator validates a block before it is accepted into the chain.
type BlockValidator interface {
	ValidateBlock(block *core.Block) error
}

// BlockExecutor applies all transactions in a block against the state.
type BlockExecutor interface {
	ExecuteBlock(block *core.Block) error
	SetEmitter(em events.EventEmitter)
}

// Syncer handles block synchronisation between nodes.
type Syncer struct {
	node             *Node
	bc               *core.Blockchain
	validator        BlockValidator
	exec             BlockExecutor // may be nil; if set, state is also required
	state            core.State    // may be nil; used with exec to commit after each block
	emitter          events.EventEmitter
	mempool          *core.Mempool // may be nil; used to remove committed txs after sync
	syncBatchSize    int
	maxSyncBatchSize int
}

// NewSyncer creates a Syncer that requests missing blocks from peers.
// Pass non-nil exec and state so that synced blocks are fully applied to the
// local state; without them the node will have blocks but no account/asset state.
// syncBatchSize and maxSyncBatchSize control how many blocks are requested per round.
func NewSyncer(node *Node, bc *core.Blockchain, validator BlockValidator, exec BlockExecutor, state core.State, emitter events.EventEmitter, mempool *core.Mempool, syncBatchSize, maxSyncBatchSize int) *Syncer {
	s := &Syncer{node: node, bc: bc, validator: validator, exec: exec, state: state, emitter: emitter, mempool: mempool, syncBatchSize: syncBatchSize, maxSyncBatchSize: maxSyncBatchSize}
	node.Handle(MsgHello, s.handleHello)
	node.Handle(MsgGetBlocks, s.handleGetBlocks)
	node.Handle(MsgBlocks, s.handleBlocks)
	node.Handle(MsgBlock, s.handleBlock)
	return s
}

// handleHello triggers an initial block sync when a peer announces itself.
// If the peer declares a node_id different from its current ID, re-key the
// peer map so future lookups use the declared identity.
func (s *Syncer) handleHello(peer *Peer, msg Message) {
	var hello struct {
		NodeID string `json:"node_id"`
	}
	if msg.Payload != nil {
		if err := json.Unmarshal(msg.Payload, &hello); err != nil {
			slog.Warn("malformed hello payload", "component", "sync", "peer", peer.ID, "error", err)
		}
	}
	if hello.NodeID != "" && hello.NodeID != peer.ID {
		oldID := peer.ID
		s.node.ReKeyPeer(oldID, hello.NodeID)
		slog.Info("peer re-keyed after hello", "component", "sync", "old_id", oldID, "new_id", hello.NodeID)
	}

	fromHeight := s.bc.Height() + 1
	if err := s.RequestBlocks(peer, fromHeight); err != nil {
		slog.Error("failed to request blocks", "component", "sync", "peer", peer.ID, "error", err)
	}
}

// SyncWithPeer requests missing blocks from the given peer.
// Call this after AddPeer to initiate an outbound sync.
func (s *Syncer) SyncWithPeer(peer *Peer) {
	fromHeight := s.bc.Height() + 1
	if err := s.RequestBlocks(peer, fromHeight); err != nil {
		slog.Error("failed to request blocks", "component", "sync", "peer", peer.ID, "error", err)
	}
}

// RequestBlocks asks peer for blocks starting at fromHeight.
func (s *Syncer) RequestBlocks(peer *Peer, fromHeight int64) error {
	req, err := json.Marshal(GetBlocksRequest{FromHeight: fromHeight, Limit: s.syncBatchSize})
	if err != nil {
		return err
	}
	return peer.Send(Message{Type: MsgGetBlocks, Payload: req})
}

func (s *Syncer) handleGetBlocks(peer *Peer, msg Message) {
	var req GetBlocksRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		slog.Error("unmarshal GetBlocks request", "component", "sync", "peer", peer.ID, "error", err)
		return
	}
	if req.Limit <= 0 || req.Limit > s.maxSyncBatchSize {
		req.Limit = s.syncBatchSize
	}
	blocks, _ := s.bc.GetBlockRange(req.FromHeight, req.Limit)
	data, err := json.Marshal(BlocksResponse{Blocks: blocks})
	if err != nil {
		slog.Error("marshal blocks response", "component", "sync", "error", err)
		return
	}
	if err := peer.Send(Message{Type: MsgBlocks, Payload: data}); err != nil {
		slog.Error("send blocks failed", "component", "sync", "peer", peer.ID, "error", err)
	}
}

func (s *Syncer) handleBlocks(peer *Peer, msg Message) {
	var resp BlocksResponse
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		slog.Error("unmarshal Blocks response", "component", "sync", "peer", peer.ID, "error", err)
		return
	}
	for _, b := range resp.Blocks {
		if s.validator != nil {
			if err := s.validator.ValidateBlock(b); err != nil {
				slog.Error("block validation failed", "component", "sync", "height", b.Header.Height, "error", err)
				return // stop processing blocks from this peer
			}
		}

		// Acquire block-execution write lock to prevent RPC from seeing
		// partially-applied state during execution.
		if s.state != nil {
			s.state.BlockLock()
		}

		// Take a snapshot so we can revert if AddBlock fails.
		var snapID int
		var buf *events.Buffer
		if s.exec != nil && s.state != nil {
			var err error
			snapID, err = s.state.Snapshot()
			if err != nil {
				s.state.BlockUnlock()
				slog.Error("block snapshot failed", "component", "sync", "height", b.Header.Height, "error", err)
				continue
			}
			// Buffer events during execution so they are only delivered
			// after a successful commit.
			buf = events.NewBuffer()
			s.exec.SetEmitter(buf)
			if err := s.exec.ExecuteBlock(b); err != nil {
				s.exec.SetEmitter(s.emitter)
				buf.Discard()
				if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
					slog.Error("FATAL: revert failed after exec error", "component", "sync", "height", b.Header.Height, "revert_error", revErr, "exec_error", err)
				os.Exit(1)
				}
				s.state.BlockUnlock()
				slog.Error("block execution failed", "component", "sync", "height", b.Header.Height, "error", err)
				return
			}
			s.exec.SetEmitter(s.emitter)
		}

		// (A) Verify state root matches after execution.
		if s.exec != nil && s.state != nil {
			computedRoot := s.state.ComputeRoot()
			if b.Header.StateRoot != "" && computedRoot != b.Header.StateRoot {
				buf.Discard()
				if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
					slog.Error("FATAL: revert failed after state root mismatch", "component", "sync", "height", b.Header.Height, "error", revErr)
				os.Exit(1)
				}
				s.state.BlockUnlock()
				slog.Error("block state root mismatch", "component", "sync", "height", b.Header.Height, "computed", computedRoot, "want", b.Header.StateRoot)
				return
			}
		}

		if err := s.bc.AddBlock(b); err != nil {
			if s.exec != nil && s.state != nil {
				buf.Discard()
				if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
					slog.Error("FATAL: revert failed after add error", "component", "sync", "height", b.Header.Height, "revert_error", revErr, "add_error", err)
				os.Exit(1)
				}
			}
			if s.state != nil {
				s.state.BlockUnlock()
			}
			slog.Error("block add failed", "component", "sync", "height", b.Header.Height, "error", err)
			return
		}

		if s.exec != nil && s.state != nil {
			if err := s.state.Commit(); err != nil {
				slog.Error("FATAL: block state commit failed", "component", "sync", "height", b.Header.Height, "error", err)
			os.Exit(1)
			}
			// Flush buffered events now that the block is committed.
			if buf != nil {
				buf.Flush(s.emitter)
			}
		}

		if s.state != nil {
			s.state.BlockUnlock()
		}

		// Remove synced transactions from the mempool.
		if s.mempool != nil && len(b.Transactions) > 0 {
			txIDs := make([]string, len(b.Transactions))
			for i, tx := range b.Transactions {
				txIDs[i] = tx.ID
			}
			s.mempool.Remove(txIDs)
		}
	}

	// If we received a full batch, there may be more blocks — keep requesting.
	if len(resp.Blocks) >= s.syncBatchSize {
		nextHeight := s.bc.Height() + 1
		if err := s.RequestBlocks(peer, nextHeight); err != nil {
			slog.Error("follow-up request failed", "component", "sync", "peer", peer.ID, "error", err)
		}
	}
}

// handleBlock processes a single block broadcast from a proposer in real-time.
// It only accepts the block if it is exactly the next expected block (tip+1);
// otherwise it is silently ignored (the node already has it, or needs batch sync).
func (s *Syncer) handleBlock(_ *Peer, msg Message) {
	var b core.Block
	if err := json.Unmarshal(msg.Payload, &b); err != nil {
		slog.Error("unmarshal block", "component", "sync", "error", err)
		return
	}

	// Only process if this is the next expected block.
	expected := s.bc.Height() + 1
	if b.Header.Height != expected {
		return
	}

	if s.validator != nil {
		if err := s.validator.ValidateBlock(&b); err != nil {
			slog.Error("block validation failed", "component", "sync", "height", b.Header.Height, "error", err)
			return
		}
	}

	// Acquire block-execution write lock to prevent RPC from seeing
	// partially-applied state during execution.
	if s.state != nil {
		s.state.BlockLock()
	}

	var snapID int
	var buf *events.Buffer
	if s.exec != nil && s.state != nil {
		var err error
		snapID, err = s.state.Snapshot()
		if err != nil {
			s.state.BlockUnlock()
			slog.Error("block snapshot failed", "component", "sync", "height", b.Header.Height, "error", err)
			return
		}
		buf = events.NewBuffer()
		s.exec.SetEmitter(buf)
		if err := s.exec.ExecuteBlock(&b); err != nil {
			s.exec.SetEmitter(s.emitter)
			buf.Discard()
			if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
				slog.Error("FATAL: revert failed after exec error", "component", "sync", "height", b.Header.Height, "revert_error", revErr, "exec_error", err)
				os.Exit(1)
			}
			s.state.BlockUnlock()
			slog.Error("block execution failed", "component", "sync", "height", b.Header.Height, "error", err)
			return
		}
		s.exec.SetEmitter(s.emitter)
	}

	// Verify state root matches after execution.
	if s.exec != nil && s.state != nil {
		computedRoot := s.state.ComputeRoot()
		if b.Header.StateRoot != "" && computedRoot != b.Header.StateRoot {
			buf.Discard()
			if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
				slog.Error("FATAL: revert failed after state root mismatch", "component", "sync", "height", b.Header.Height, "error", revErr)
				os.Exit(1)
			}
			s.state.BlockUnlock()
			slog.Error("block state root mismatch", "component", "sync", "height", b.Header.Height, "computed", computedRoot, "want", b.Header.StateRoot)
			return
		}
	}

	if err := s.bc.AddBlock(&b); err != nil {
		if s.exec != nil && s.state != nil {
			buf.Discard()
			if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
				slog.Error("FATAL: revert failed after add error", "component", "sync", "height", b.Header.Height, "revert_error", revErr, "add_error", err)
				os.Exit(1)
			}
		}
		if s.state != nil {
			s.state.BlockUnlock()
		}
		slog.Error("block add failed", "component", "sync", "height", b.Header.Height, "error", err)
		return
	}

	if s.exec != nil && s.state != nil {
		if err := s.state.Commit(); err != nil {
			slog.Error("FATAL: block state commit failed", "component", "sync", "height", b.Header.Height, "error", err)
			os.Exit(1)
		}
		if buf != nil {
			buf.Flush(s.emitter)
		}
	}

	if s.state != nil {
		s.state.BlockUnlock()
	}

	// Remove committed transactions from the mempool.
	if s.mempool != nil && len(b.Transactions) > 0 {
		txIDs := make([]string, len(b.Transactions))
		for i, tx := range b.Transactions {
			txIDs[i] = tx.ID
		}
		s.mempool.Remove(txIDs)
	}

	slog.Info("accepted broadcast block", "component", "sync", "height", b.Header.Height, "hash", b.Hash)
}
