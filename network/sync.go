package network

import (
	"encoding/json"
	"log"

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
	node      *Node
	bc        *core.Blockchain
	validator BlockValidator
	exec      BlockExecutor // may be nil; if set, state is also required
	state     core.State    // may be nil; used with exec to commit after each block
	emitter   events.EventEmitter
	mempool   *core.Mempool // may be nil; used to remove committed txs after sync
}

// NewSyncer creates a Syncer that requests missing blocks from peers.
// Pass non-nil exec and state so that synced blocks are fully applied to the
// local state; without them the node will have blocks but no account/asset state.
func NewSyncer(node *Node, bc *core.Blockchain, validator BlockValidator, exec BlockExecutor, state core.State, emitter events.EventEmitter, mempool *core.Mempool) *Syncer {
	s := &Syncer{node: node, bc: bc, validator: validator, exec: exec, state: state, emitter: emitter, mempool: mempool}
	node.Handle(MsgHello, s.handleHello)
	node.Handle(MsgGetBlocks, s.handleGetBlocks)
	node.Handle(MsgBlocks, s.handleBlocks)
	return s
}

// handleHello triggers an initial block sync when a peer announces itself.
func (s *Syncer) handleHello(peer *Peer, _ Message) {
	fromHeight := s.bc.Height() + 1
	if err := s.RequestBlocks(peer, fromHeight); err != nil {
		log.Printf("[sync] failed to request blocks from %s: %v", peer.ID, err)
	}
}

// SyncWithPeer requests missing blocks from the given peer.
// Call this after AddPeer to initiate an outbound sync.
func (s *Syncer) SyncWithPeer(peer *Peer) {
	fromHeight := s.bc.Height() + 1
	if err := s.RequestBlocks(peer, fromHeight); err != nil {
		log.Printf("[sync] failed to request blocks from %s: %v", peer.ID, err)
	}
}

// RequestBlocks asks peer for blocks starting at fromHeight.
func (s *Syncer) RequestBlocks(peer *Peer, fromHeight int64) error {
	req, err := json.Marshal(GetBlocksRequest{FromHeight: fromHeight, Limit: 50})
	if err != nil {
		return err
	}
	return peer.Send(Message{Type: MsgGetBlocks, Payload: req})
}

func (s *Syncer) handleGetBlocks(peer *Peer, msg Message) {
	var req GetBlocksRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return
	}
	if req.Limit <= 0 || req.Limit > 200 {
		req.Limit = 50
	}
	blocks := make([]*core.Block, 0, req.Limit)
	for h := req.FromHeight; h < req.FromHeight+int64(req.Limit); h++ {
		b, err := s.bc.GetBlockByHeight(h)
		if err != nil {
			break
		}
		blocks = append(blocks, b)
	}
	data, err := json.Marshal(BlocksResponse{Blocks: blocks})
	if err != nil {
		log.Printf("[sync] marshal blocks response: %v", err)
		return
	}
	if err := peer.Send(Message{Type: MsgBlocks, Payload: data}); err != nil {
		log.Printf("[sync] send blocks to %s: %v", peer.ID, err)
	}
}

func (s *Syncer) handleBlocks(peer *Peer, msg Message) {
	var resp BlocksResponse
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		return
	}
	for _, b := range resp.Blocks {
		if s.validator != nil {
			if err := s.validator.ValidateBlock(b); err != nil {
				log.Printf("[sync] block %d validation failed: %v", b.Header.Height, err)
				return // stop processing blocks from this peer
			}
		}

		// Take a snapshot so we can revert if AddBlock fails.
		var snapID int
		var buf *events.Buffer
		if s.exec != nil && s.state != nil {
			var err error
			snapID, err = s.state.Snapshot()
			if err != nil {
				log.Printf("[sync] block %d snapshot failed: %v", b.Header.Height, err)
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
					log.Fatalf("[sync] FATAL: block %d revert failed after exec error: %v (exec: %v)", b.Header.Height, revErr, err)
				}
				log.Printf("[sync] block %d execution failed: %v", b.Header.Height, err)
				continue
			}
			s.exec.SetEmitter(s.emitter)
		}

		// (A) Verify state root matches after execution.
		if s.exec != nil && s.state != nil {
			computedRoot := s.state.ComputeRoot()
			if b.Header.StateRoot != "" && computedRoot != b.Header.StateRoot {
				buf.Discard()
				if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
					log.Fatalf("[sync] FATAL: block %d revert failed after state root mismatch: %v", b.Header.Height, revErr)
				}
				log.Printf("[sync] block %d state root mismatch: computed %s want %s", b.Header.Height, computedRoot, b.Header.StateRoot)
				return
			}
		}

		if err := s.bc.AddBlock(b); err != nil {
			if s.exec != nil && s.state != nil {
				buf.Discard()
				if revErr := s.state.RevertToSnapshot(snapID); revErr != nil {
					log.Fatalf("[sync] FATAL: block %d revert failed after add error: %v (add: %v)", b.Header.Height, revErr, err)
				}
			}
			log.Printf("[sync] block %d add failed: %v", b.Header.Height, err)
			continue
		}

		if s.exec != nil && s.state != nil {
			if err := s.state.Commit(); err != nil {
				log.Fatalf("[sync] FATAL: block %d state commit failed: %v", b.Header.Height, err)
			}
			// Flush buffered events now that the block is committed.
			if buf != nil {
				buf.Flush(s.emitter)
			}
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
	if len(resp.Blocks) >= 50 {
		nextHeight := s.bc.Height() + 1
		if err := s.RequestBlocks(peer, nextHeight); err != nil {
			log.Printf("[sync] follow-up request to %s failed: %v", peer.ID, err)
		}
	}
}
