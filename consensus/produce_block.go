package consensus

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/metrics"
)

// produceBlock은 produce_block 흐름의 단계 본문이다.
// 단계의 순서, 실패 시 되돌리기, 커밋 지점은 설계(design/tolchain.yaml)에서 생성한 produce_block.gen.go가 정한다.
type produceBlock struct {
	p          *PoA
	start      time.Time
	candidates []*core.Transaction
	prevHash   string
	nextHeight int64
	snapID     int
	buf        *events.Buffer
	block      *core.Block
	successTxs []*core.Transaction
	failedIDs  []string
	cause      error // 되돌리기를 부른 단계의 오류. Fatal 로그에 남긴다
}

func (s *produceBlock) fail(err error) error {
	s.cause = err
	return err
}

func (s *produceBlock) CheckProposer() error {
	if !s.p.IsProposer() {
		return errors.New("not the proposer for this round")
	}
	return nil
}

func (s *produceBlock) SelectTxs() error {
	limit := s.p.cfg.MaxBlockTxs
	if limit <= 0 {
		limit = defaultMaxBlockTxs
	}
	s.candidates = s.p.mempool.Pending(limit)
	return nil
}

// Lock은 RPC 조회(읽기 잠금)가 반쯤 적용된 상태를 보지 못하게 블록 실행 쓰기 잠금을 잡는다.
func (s *produceBlock) Lock() error {
	s.p.state.BlockLock()
	return nil
}

func (s *produceBlock) Unlock() error {
	s.p.state.BlockUnlock()
	return nil
}

// ReadTip은 잠금을 잡은 뒤에 tip을 읽어, 그 사이 sync가 받은 블록 위에 만들도록 한다.
// tip이 없으면 제네시스가 없는 것이고, 높이 1 블록은 AddBlock에서 거부된다.
func (s *produceBlock) ReadTip() error {
	if tip := s.p.bc.Tip(); tip == nil {
		s.prevHash, s.nextHeight = config.GenesisHash, 1
	} else {
		s.prevHash, s.nextHeight = tip.Hash, tip.Header.Height+1
	}
	return nil
}

func (s *produceBlock) Snapshot() error {
	id, err := s.p.state.Snapshot()
	if err != nil {
		return s.fail(fmt.Errorf("block snapshot: %w", err))
	}
	s.snapID = id
	return nil
}

func (s *produceBlock) Revert() error {
	return s.p.state.RevertToSnapshot(s.snapID)
}

// BufferEvents는 실행 중 이벤트를 모아 두었다가 커밋 뒤에만 내보내도록 한다.
func (s *produceBlock) BufferEvents() error {
	s.buf = events.NewBuffer()
	s.p.exec.SetEmitter(s.buf)
	return nil
}

func (s *produceBlock) DiscardEvents() error {
	s.p.exec.SetEmitter(s.p.emitter)
	s.buf.Discard()
	return nil
}

// ExecuteTxs는 tx를 하나씩 실행하고 성공한 것만 남긴다. 실패한 tx는 건너뛴다.
func (s *produceBlock) ExecuteTxs() error {
	s.block = core.NewBlock(s.p.cfg.Genesis.ChainID, s.nextHeight, s.prevHash, s.p.pubKey.Hex(), s.candidates)
	for _, tx := range s.candidates {
		if txErr := s.p.exec.ExecuteTx(s.block, tx); txErr != nil {
			slog.Warn("skipping failed tx", "component", "consensus", "tx_id", tx.ID, "error", txErr)
			metrics.TxFailed.Inc()
			if !s.waitsForEarlierNonce(tx, txErr) {
				s.failedIDs = append(s.failedIDs, tx.ID)
			}
			continue
		}
		s.successTxs = append(s.successTxs, tx)
	}
	return nil
}

func (s *produceBlock) waitsForEarlierNonce(tx *core.Transaction, txErr error) bool {
	if !errors.Is(txErr, core.ErrInvalidNonce) {
		return false
	}
	acc, err := s.p.state.GetAccount(tx.NonceAccount())
	return err == nil && tx.Nonce > acc.Nonce
}

func (s *produceBlock) RestoreEmitter() error {
	s.p.exec.SetEmitter(s.p.emitter)
	return nil
}

// ComputeRoot는 실행에 쓴 임시 블록을 최종 블록으로 쓴다. 핸들러가 본 시각을 그대로 담아야
// 동기화하는 노드가 다시 실행할 때 같은 StateRoot가 나온다. 루트는 쓰기 버퍼에서 계산해,
// AddBlock이 실패해도 상태가 아직 저장되지 않은 채로 남게 한다.
func (s *produceBlock) ComputeRoot() error {
	s.block.Transactions = s.successTxs
	s.block.Header.TxRoot = core.ComputeTxRoot(s.successTxs)
	stateRoot, err := s.p.state.ComputeRoot()
	if err != nil {
		return s.fail(fmt.Errorf("compute state root: %w", err))
	}
	s.block.Header.StateRoot = stateRoot
	return nil
}

func (s *produceBlock) Sign() error {
	s.block.Sign(s.p.privKey)
	return nil
}

// Validate는 피어가 할 검사를 브로드캐스트 전에 스스로 돌려 제안자 버그를 먼저 잡는다.
func (s *produceBlock) Validate() error {
	if err := s.p.ValidateBlock(s.block); err != nil {
		return s.fail(fmt.Errorf("self-validation: %w", err))
	}
	return nil
}

func (s *produceBlock) AddBlock() error {
	if err := s.p.bc.AddBlock(s.block); err != nil {
		return s.fail(fmt.Errorf("add block: %w", err))
	}
	return nil
}

// CommitState는 블록이 저장된 뒤에만 상태를 반영한다.
func (s *produceBlock) CommitState() error {
	return s.p.state.Commit()
}

func (s *produceBlock) FlushEvents() error {
	s.buf.Flush(s.p.emitter)
	return nil
}

// EmitBlockCommit은 Sign 뒤라 block.Hash가 채워진 상태에서 보낸다.
func (s *produceBlock) EmitBlockCommit() error {
	s.p.emitter.Emit(events.Event{
		Type:        events.EventBlockCommit,
		BlockHeight: s.block.Header.Height,
		Data:        events.BlockCommitData{Hash: s.block.Hash, Txs: len(s.block.Transactions), Block: s.block}.Map(),
	})
	return nil
}

// RemoveFromMempool은 실패한 tx를 커밋 뒤에만 지운다. 더 일찍 지우면 블록 생산이 중단돼서
// 실패한 tx(stale tip, AddBlock 실패 등)까지 영영 사라진다.
func (s *produceBlock) RemoveFromMempool() error {
	txIDs := make([]string, len(s.successTxs))
	for i, tx := range s.successTxs {
		txIDs[i] = tx.ID
	}
	s.p.mempool.Remove(txIDs)
	if len(s.failedIDs) > 0 {
		s.p.mempool.Remove(s.failedIDs)
	}
	return nil
}

func (s *produceBlock) RecordMetrics() error {
	metrics.BlocksProduced.Inc()
	metrics.BlockHeight.Set(float64(s.block.Header.Height))
	metrics.BlockProductionDuration.Observe(time.Since(s.start).Seconds())
	metrics.BlockTxCount.Observe(float64(len(s.successTxs)))
	return nil
}

// Fatal은 되돌리기나 커밋이 실패해 노드 상태를 믿을 수 없을 때 프로세스를 끝낸다.
func (s *produceBlock) Fatal(step string, err error) {
	slog.Error("FATAL: block production cannot recover", "component", "consensus", "step", step, "height", s.nextHeight, "error", err, "cause", s.cause)
	os.Exit(1)
}
