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

// Syncer handles block synchronisation between nodes.
type Syncer struct {
	node      *Node
	bc        *core.Blockchain
	validator BlockValidator
}

// NewSyncer creates a Syncer that requests missing blocks from peers.
// validator may be nil (blocks are accepted without validation), but a non-nil
// validator (e.g. consensus.PoA) is strongly recommended for production use.
func NewSyncer(node *Node, bc *core.Blockchain, validator BlockValidator) *Syncer {
	s := &Syncer{node: node, bc: bc, validator: validator}
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
				return
			}
		}
		if err := s.bc.AddBlock(b); err != nil {
			log.Printf("[sync] block %d add failed: %v", b.Header.Height, err)
		}
	}
}
