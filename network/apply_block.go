package network

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
)

// applyBlock은 apply_block 흐름의 단계 본문이다.
// 단계의 순서, 실패 시 되돌리기, 커밋 지점은 설계(design/tolchain.yaml)에서 생성한 apply_block.gen.go가 정한다.
// 실행기나 상태가 없는 노드에서는 실행·스냅샷·루트 단계가 아무것도 하지 않는다.
type applyBlock struct {
	s       *Syncer
	b       *core.Block
	snapID  int
	snapped bool
	buf     *events.Buffer
	root    string
	cause   error // 되돌리기를 부른 단계의 오류. Fatal 로그에 남긴다
}

func (a *applyBlock) executing() bool { return a.s.exec != nil && a.s.state != nil }

func (a *applyBlock) fail(err error) error {
	a.cause = err
	return err
}

func (a *applyBlock) ValidateBlock() error {
	if a.s.validator != nil {
		return a.s.validator.ValidateBlock(a.b)
	}
	return nil
}

// Lock은 RPC가 반쯤 적용된 상태를 보지 못하게 블록 실행 쓰기 잠금을 잡는다.
func (a *applyBlock) Lock() error {
	if a.s.state != nil {
		a.s.state.BlockLock()
	}
	return nil
}

func (a *applyBlock) Unlock() error {
	if a.s.state != nil {
		a.s.state.BlockUnlock()
	}
	return nil
}

// Snapshot은 AddBlock이 실패하면 되돌릴 수 있게 스냅샷을 잡는다.
func (a *applyBlock) Snapshot() error {
	if !a.executing() {
		return nil
	}
	id, err := a.s.state.Snapshot()
	if err != nil {
		return a.fail(err)
	}
	a.snapID, a.snapped = id, true
	return nil
}

func (a *applyBlock) Revert() error {
	if !a.snapped {
		return nil
	}
	return a.s.state.RevertToSnapshot(a.snapID)
}

// BufferEvents는 실행 중 이벤트를 모아 두었다가 커밋 뒤에만 내보내도록 한다.
func (a *applyBlock) BufferEvents() error {
	if !a.executing() {
		return nil
	}
	a.buf = events.NewBuffer()
	a.s.exec.SetEmitter(a.buf)
	return nil
}

func (a *applyBlock) DiscardEvents() error {
	if a.buf == nil {
		return nil
	}
	a.s.exec.SetEmitter(a.s.emitter)
	a.buf.Discard()
	return nil
}

func (a *applyBlock) ExecuteBlock() error {
	if !a.executing() {
		return nil
	}
	if err := a.s.exec.ExecuteBlock(a.b); err != nil {
		return a.fail(err)
	}
	return nil
}

func (a *applyBlock) RestoreEmitter() error {
	if a.executing() {
		a.s.exec.SetEmitter(a.s.emitter)
	}
	return nil
}

func (a *applyBlock) ComputeRoot() error {
	if !a.executing() {
		return nil
	}
	root, err := a.s.state.ComputeRoot()
	if err != nil {
		return a.fail(err)
	}
	a.root = root
	return nil
}

// errRootMismatch는 실행 결과의 상태 루트가 블록 헤더와 다르다는 뜻이다.
type errRootMismatch struct{ computed, want string }

func (e *errRootMismatch) Error() string {
	return fmt.Sprintf("state root mismatch: computed %s, want %s", e.computed, e.want)
}

func (a *applyBlock) CheckRoot() error {
	if !a.executing() {
		return nil
	}
	if a.b.Header.StateRoot != "" && a.root != a.b.Header.StateRoot {
		return a.fail(&errRootMismatch{a.root, a.b.Header.StateRoot})
	}
	return nil
}

func (a *applyBlock) AddBlock() error {
	if err := a.s.bc.AddBlock(a.b); err != nil {
		return a.fail(err)
	}
	return nil
}

func (a *applyBlock) CommitState() error {
	if !a.executing() {
		return nil
	}
	return a.s.state.Commit()
}

// FlushEvents는 블록이 커밋된 뒤 모아 둔 이벤트를 내보낸다.
func (a *applyBlock) FlushEvents() error {
	if a.executing() && a.buf != nil {
		a.buf.Flush(a.s.emitter)
	}
	return nil
}

func (a *applyBlock) RemoveFromMempool() error {
	if a.s.mempool != nil && len(a.b.Transactions) > 0 {
		txIDs := make([]string, len(a.b.Transactions))
		for i, tx := range a.b.Transactions {
			txIDs[i] = tx.ID
		}
		a.s.mempool.Remove(txIDs)
	}
	return nil
}

// Fatal은 되돌리기나 커밋이 실패해 노드 상태를 믿을 수 없을 때 프로세스를 끝낸다.
func (a *applyBlock) Fatal(step string, err error) {
	slog.Error("FATAL: block apply cannot recover", "component", "sync", "step", step, "height", a.b.Header.Height, "error", err, "cause", a.cause)
	os.Exit(1)
}

// applyFailure는 apply_block이 실패한 단계에 맞는 로그를 남긴다.
func applyFailure(err error, b *core.Block) (step string) {
	var fe *FlowError
	if !errors.As(err, &fe) {
		slog.Error("block apply failed", "component", "sync", "height", b.Header.Height, "error", err)
		return ""
	}
	msg := map[string]string{
		"validate_block": "block validation failed",
		"snapshot":       "block snapshot failed",
		"execute_block":  "block execution failed",
		"compute_root":   "state root computation failed",
		"check_root":     "block state root mismatch",
		"add_block":      "block add failed",
	}[fe.Step]
	if msg == "" {
		msg = "block apply failed at " + fe.Step
	}
	var mismatch *errRootMismatch
	if errors.As(fe.Err, &mismatch) {
		slog.Error(msg, "component", "sync", "height", b.Header.Height, "computed", mismatch.computed, "want", mismatch.want)
	} else {
		slog.Error(msg, "component", "sync", "height", b.Header.Height, "error", fe.Err)
	}
	return fe.Step
}
