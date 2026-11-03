package network

import (
	"encoding/json"
	"log/slog"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
)

type GetBlocksRequest struct {
	FromHeight int64 `json:"from_height"`
	Limit      int   `json:"limit"`
}

type BlocksResponse struct {
	Blocks []*core.Block `json:"blocks"`
}

type BlockValidator interface {
	ValidateBlock(block *core.Block) error
}

type BlockExecutor interface {
	ExecuteBlock(block *core.Block) error
	SetEmitter(em events.EventEmitter)
}

// Syncer fetches missing blocks from peers and applies blocks they broadcast.
type Syncer struct {
	node      *Node
	bc        *core.Blockchain
	validator BlockValidator
	// exec and state are both nil on a node that stores blocks without
	// executing them.
	exec             BlockExecutor
	state            core.State
	emitter          events.EventEmitter
	mempool          *core.Mempool // nil → nothing to prune after applying
	syncBatchSize    int
	maxSyncBatchSize int
}

func NewSyncer(node *Node, bc *core.Blockchain, validator BlockValidator, exec BlockExecutor, state core.State, emitter events.EventEmitter, mempool *core.Mempool, syncBatchSize, maxSyncBatchSize int) *Syncer {
	s := &Syncer{node: node, bc: bc, validator: validator, exec: exec, state: state, emitter: emitter, mempool: mempool, syncBatchSize: syncBatchSize, maxSyncBatchSize: maxSyncBatchSize}
	node.Handle(MsgHello, s.handleHello)
	node.Handle(MsgGetBlocks, s.handleGetBlocks)
	node.Handle(MsgBlocks, s.handleBlocks)
	node.Handle(MsgBlock, s.handleBlock)
	return s
}

// handleHello re-keys the peer under its announced node_id and starts a sync.
func (s *Syncer) handleHello(peer *Peer, msg Message) {
	var hello struct {
		NodeID string `json:"node_id"`
	}
	if msg.Payload != nil {
		if err := json.Unmarshal(msg.Payload, &hello); err != nil {
			slog.Warn("malformed hello payload", "component", "sync", "peer", peer.ID(), "error", err)
		}
	}
	if hello.NodeID != "" && hello.NodeID != peer.ID() {
		oldID := peer.ID()
		s.node.ReKeyPeer(oldID, hello.NodeID)
		slog.Info("peer re-keyed after hello", "component", "sync", "old_id", oldID, "new_id", hello.NodeID)
	}
	s.SyncWithPeer(peer)
}

// SyncWithPeer asks peer for every block above our tip.
func (s *Syncer) SyncWithPeer(peer *Peer) {
	fromHeight := s.bc.Height() + 1
	if err := s.RequestBlocks(peer, fromHeight); err != nil {
		slog.Error("failed to request blocks", "component", "sync", "peer", peer.ID(), "error", err)
	}
}

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
		slog.Error("unmarshal GetBlocks request", "component", "sync", "peer", peer.ID(), "error", err)
		return
	}
	if req.Limit <= 0 || req.Limit > s.maxSyncBatchSize {
		req.Limit = s.syncBatchSize
	}
	blocks, err := s.bc.GetBlockRange(req.FromHeight, req.Limit)
	if err != nil {
		slog.Error("read blocks for peer", "component", "sync", "peer", peer.ID(), "from", req.FromHeight, "error", err)
		return
	}
	data, err := json.Marshal(BlocksResponse{Blocks: blocks})
	if err != nil {
		slog.Error("marshal blocks response", "component", "sync", "error", err)
		return
	}
	if err := peer.Send(Message{Type: MsgBlocks, Payload: data}); err != nil {
		slog.Error("send blocks failed", "component", "sync", "peer", peer.ID(), "error", err)
	}
}

func (s *Syncer) handleBlocks(peer *Peer, msg Message) {
	var resp BlocksResponse
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		slog.Error("unmarshal Blocks response", "component", "sync", "peer", peer.ID(), "error", err)
		return
	}
	for _, b := range resp.Blocks {
		if err := RunApplyBlock(&applyBlock{s: s, b: b}); err != nil {
			// 스냅샷 실패는 이 블록만 건너뛴다. 그 밖의 실패는 이 피어의 블록 처리를 멈춘다.
			if applyFailure(err, b) == "snapshot" {
				continue
			}
			return
		}
	}

	if len(resp.Blocks) >= s.syncBatchSize {
		nextHeight := s.bc.Height() + 1
		if err := s.RequestBlocks(peer, nextHeight); err != nil {
			slog.Error("follow-up request failed", "component", "sync", "peer", peer.ID(), "error", err)
		}
	}
}

// handleBlock applies a proposer's broadcast only if it extends our tip;
// anything else is either known already or left to batch sync.
func (s *Syncer) handleBlock(_ *Peer, msg Message) {
	var b core.Block
	if err := json.Unmarshal(msg.Payload, &b); err != nil {
		slog.Error("unmarshal block", "component", "sync", "error", err)
		return
	}

	expected := s.bc.Height() + 1
	if b.Header.Height != expected {
		return
	}

	if err := RunApplyBlock(&applyBlock{s: s, b: &b}); err != nil {
		applyFailure(err, &b)
		return
	}

	slog.Info("accepted broadcast block", "component", "sync", "height", b.Header.Height, "hash", b.Hash)
}
