package storage_test

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/tolelom/tolchain/core"
)

func TestSnapshot_CostDoesNotGrowWithWriteBuffer(t *testing.T) {
	s := newTestState()
	for i := 0; i < 5000; i++ {
		s.SetAccount(&core.Account{Address: fmt.Sprintf("acct-%d", i), Balance: 1})
	}
	allocs := testing.AllocsPerRun(50, func() {
		if _, err := s.Snapshot(); err != nil {
			t.Fatal(err)
		}
	})
	if allocs > 1 {
		t.Fatalf("Snapshot allocated %.0f times with 5000 buffered writes; want O(1)", allocs)
	}
}

func TestSnapshot_RandomOperationsMatchCopyModel(t *testing.T) {
	for seed := int64(1); seed <= 200; seed++ {
		runSnapshotModel(t, seed)
	}
}

func runSnapshotModel(t *testing.T, seed int64) {
	r := rand.New(rand.NewSource(seed))
	s := newTestState()
	ids := []string{"a", "b", "c", "d", "e"}
	model := map[string]string{}
	for _, id := range ids[:3] {
		s.SetAsset(&core.Asset{ID: id, Owner: "genesis-" + id})
		model[id] = "genesis-" + id
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	var stack []map[string]string
	var snapIDs []int
	for step := 0; step < 60; step++ {
		id := ids[r.Intn(len(ids))]
		switch op := r.Intn(10); {
		case op < 4:
			owner := fmt.Sprintf("o%d", step)
			s.SetAsset(&core.Asset{ID: id, Owner: owner})
			model[id] = owner
		case op < 6:
			s.DeleteAsset(id)
			delete(model, id)
		case op < 8:
			snapID, err := s.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			cp := make(map[string]string, len(model))
			for k, v := range model {
				cp[k] = v
			}
			stack = append(stack, cp)
			snapIDs = append(snapIDs, snapID)
		default:
			if len(stack) == 0 {
				continue
			}
			at := r.Intn(len(stack))
			if err := s.RevertToSnapshot(snapIDs[at]); err != nil {
				t.Fatalf("seed %d step %d: revert: %v", seed, step, err)
			}
			model = stack[at]
			stack, snapIDs = stack[:at], snapIDs[:at]
		}
		for _, id := range ids {
			got, err := s.GetAsset(id)
			want, exists := model[id]
			switch {
			case !exists && !errors.Is(err, core.ErrNotFound):
				t.Fatalf("seed %d step %d: asset %s should be absent, got %v %v", seed, step, id, got, err)
			case exists && (err != nil || got.Owner != want):
				t.Fatalf("seed %d step %d: asset %s want owner %q, got %v %v", seed, step, id, want, got, err)
			}
		}
	}
}
