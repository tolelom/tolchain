package network

import (
	"errors"
	"reflect"
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
)

// recState는 apply_block이 부르는 상태 메서드만 기록한다. 나머지는 쓰지 않는다.
type recState struct {
	core.State
	calls            *[]string
	snapErr, rootErr error
	root             string
	revertErr        error
}

func (s *recState) BlockLock()   { *s.calls = append(*s.calls, "lock") }
func (s *recState) BlockUnlock() { *s.calls = append(*s.calls, "unlock") }
func (s *recState) Snapshot() (int, error) {
	*s.calls = append(*s.calls, "snapshot")
	return 7, s.snapErr
}
func (s *recState) RevertToSnapshot(id int) error {
	*s.calls = append(*s.calls, "revert")
	return s.revertErr
}
func (s *recState) ComputeRoot() (string, error) {
	*s.calls = append(*s.calls, "compute_root")
	return s.root, s.rootErr
}
func (s *recState) Commit() error {
	*s.calls = append(*s.calls, "commit")
	return nil
}

type recExec struct {
	calls   *[]string
	emitter events.EventEmitter
	execErr error
}

func (e *recExec) ExecuteBlock(*core.Block) error {
	*e.calls = append(*e.calls, "execute")
	return e.execErr
}

func (e *recExec) SetEmitter(em events.EventEmitter) {
	if em == e.emitter {
		*e.calls = append(*e.calls, "emitter:real")
	} else {
		*e.calls = append(*e.calls, "emitter:buffer")
	}
}

// 기대 순서는 흐름으로 옮기기 전 sync.go(29c1d3a)의 실패 경로다.
// 되돌리기의 discard_events는 이미 복원된 emitter를 한 번 더 설정한다(같은 값이라 동작은 같다).
func TestApplyBlockFailurePaths(t *testing.T) {
	boom := errors.New("boom")
	emitter := events.NewEmitter()
	cases := []struct {
		name string
		st   recState
		exec recExec
		// 헤더의 StateRoot
		headerRoot string
		step       string
		want       []string
	}{
		{name: "스냅샷 실패", st: recState{snapErr: boom}, step: "snapshot",
			want: []string{"lock", "snapshot", "unlock"}},
		{name: "실행 실패", exec: recExec{execErr: boom}, step: "execute_block",
			want: []string{"lock", "snapshot", "emitter:buffer", "execute", "emitter:real", "revert", "unlock"}},
		{name: "루트 계산 실패", st: recState{rootErr: boom}, step: "compute_root",
			want: []string{"lock", "snapshot", "emitter:buffer", "execute", "emitter:real", "compute_root", "emitter:real", "revert", "unlock"}},
		{name: "루트 불일치", st: recState{root: "a"}, headerRoot: "b", step: "check_root",
			want: []string{"lock", "snapshot", "emitter:buffer", "execute", "emitter:real", "compute_root", "emitter:real", "revert", "unlock"}},
		{name: "저장 실패", st: recState{root: "a"}, step: "add_block",
			want: []string{"lock", "snapshot", "emitter:buffer", "execute", "emitter:real", "compute_root", "emitter:real", "revert", "unlock"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var calls []string
			st, ex := c.st, c.exec
			st.calls, ex.calls, ex.emitter = &calls, &calls, emitter
			bc := core.NewBlockchain(testutil.NewMemBlockStore())
			if err := bc.Init(); err != nil {
				t.Fatal(err)
			}
			s := &Syncer{bc: bc, exec: &ex, state: &st, emitter: emitter}
			// 빈 체인에 높이 5 블록은 AddBlock이 거부한다
			b := &core.Block{Header: core.BlockHeader{Height: 5, StateRoot: c.headerRoot}}
			err := RunApplyBlock(&applyBlock{s: s, b: b})
			var fe *FlowError
			if !errors.As(err, &fe) || fe.Step != c.step {
				t.Fatalf("step: %v", err)
			}
			if !reflect.DeepEqual(calls, c.want) {
				t.Fatalf("\n got %v\nwant %v", calls, c.want)
			}
		})
	}
}

// 실행기나 상태가 없는 노드는 잠금과 저장만 한다.
func TestApplyBlockWithoutExecutor(t *testing.T) {
	var calls []string
	st := recState{calls: &calls}
	bc := core.NewBlockchain(testutil.NewMemBlockStore())
	bc.Init()
	s := &Syncer{bc: bc, state: &st}
	b := &core.Block{Header: core.BlockHeader{Height: 5}}
	err := RunApplyBlock(&applyBlock{s: s, b: b})
	var fe *FlowError
	if !errors.As(err, &fe) || fe.Step != "add_block" {
		t.Fatalf("%v", err)
	}
	if want := []string{"lock", "unlock"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("got %v", calls)
	}
}
