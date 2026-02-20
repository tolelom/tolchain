package network

import (
	"encoding/json"
	"log"

	"github.com/tolelom/tolchain/core"
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
}

// Syncer handles block synchronisation between nodes.
type Syncer struct {
	node      *Node
	bc        *core.Blockchain
	validator BlockValidator
	exec      BlockExecutor // may be nil; if set, state is also required
	state     core.State    // may be nil; used with exec to commit after each block
}

// NewSyncer creates a Syncer that requests missing blocks from peers.
// Pass non-nil exec and state so that synced blocks are fully applied to the
// local state; without them the node will have blocks but no account/asset state.
func NewSyncer(node *Node, bc *core.Blockchain, validator BlockValidator, exec BlockExecutor, state core.State) *Syncer {
	s := &Syncer{node: node, bc: bc, validator: validator, exec: exec, state: state}
	node.Handle(MsgGetBlocks, s.handleGetBlocks)
	node.Handle(MsgBlocks, s.handleBlocks)
	return s
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
		return
	}
	_ = peer.Send(Message{Type: MsgBlocks, Payload: data})
}

func (s *Syncer) handleBlocks(_ *Peer, msg Message) {
	var resp BlocksResponse
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		return
	}
	for _, b := range resp.Blocks {
		if s.validator != nil {
			if err := s.validator.ValidateBlock(b); err != nil {
				log.Printf("[sync] block %d validation failed: %v", b.Header.Height, err)
				continue // skip this block, try the rest
			}
		}

		// Take a snapshot so we can revert if AddBlock fails.
		var snapID int
		if s.exec != nil && s.state != nil {
			var err error
			snapID, err = s.state.Snapshot()
			if err != nil {
				log.Printf("[sync] block %d snapshot failed: %v", b.Header.Height, err)
				continue
			}
			if err := s.exec.ExecuteBlock(b); err != nil {
				_ = s.state.RevertToSnapshot(snapID)
				log.Printf("[sync] block %d execution failed: %v", b.Header.Height, err)
				continue
			}
		}

		if err := s.bc.AddBlock(b); err != nil {
			if s.exec != nil && s.state != nil {
				_ = s.state.RevertToSnapshot(snapID)
			}
			log.Printf("[sync] block %d add failed: %v", b.Header.Height, err)
			continue
		}

		if s.exec != nil && s.state != nil {
			if err := s.state.Commit(); err != nil {
				log.Fatalf("[sync] FATAL: block %d state commit failed: %v", b.Header.Height, err)
			}
		}
	}
}
