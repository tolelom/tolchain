package storage_test

import (
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/storage"
)

func newTestState() *storage.StateDB {
	return testutil.NewStateDB()
}

func TestStateDB_Account_DefaultZero(t *testing.T) {
	s := newTestState()
	acc, err := s.GetAccount("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if acc.Balance != 0 || acc.Nonce != 0 {
		t.Fatal("new account should have zero balance and nonce")
	}
	if acc.Address != "nonexistent" {
		t.Fatal("address should be preserved")
	}
}

func TestStateDB_Account_SetAndGet(t *testing.T) {
	s := newTestState()
	acc := &core.Account{Address: "alice", Balance: 1000, Nonce: 5}
	if err := s.SetAccount(acc); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAccount("alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance != 1000 || got.Nonce != 5 {
		t.Fatalf("expected 1000/5, got %d/%d", got.Balance, got.Nonce)
	}
}

func TestStateDB_Asset_NotFound(t *testing.T) {
	s := newTestState()
	_, err := s.GetAsset("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent asset")
	}
}

func TestStateDB_Asset_SetGetDelete(t *testing.T) {
	s := newTestState()
	asset := &core.Asset{ID: "a1", TemplateID: "t1", Owner: "alice"}
	s.SetAsset(asset)

	got, err := s.GetAsset("a1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Owner != "alice" {
		t.Fatal("owner mismatch")
	}

	s.DeleteAsset("a1")
	_, err = s.GetAsset("a1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestStateDB_Snapshot_Revert(t *testing.T) {
	s := newTestState()
	s.SetAccount(&core.Account{Address: "alice", Balance: 100})

	snapID, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}

	// Modify after snapshot.
	s.SetAccount(&core.Account{Address: "alice", Balance: 999})

	// Verify modification.
	acc, _ := s.GetAccount("alice")
	if acc.Balance != 999 {
		t.Fatal("expected 999 after modification")
	}

	// Revert.
	if err := s.RevertToSnapshot(snapID); err != nil {
		t.Fatal(err)
	}
	acc, _ = s.GetAccount("alice")
	if acc.Balance != 100 {
		t.Fatalf("expected 100 after revert, got %d", acc.Balance)
	}
}

func TestStateDB_Snapshot_InvalidID(t *testing.T) {
	s := newTestState()
	if err := s.RevertToSnapshot(99); err == nil {
		t.Fatal("expected error for invalid snapshot ID")
	}
}

func TestStateDB_Snapshot_NestedRevert(t *testing.T) {
	s := newTestState()
	s.SetAccount(&core.Account{Address: "a", Balance: 10})

	snap1, _ := s.Snapshot()
	s.SetAccount(&core.Account{Address: "a", Balance: 20})

	snap2, _ := s.Snapshot()
	s.SetAccount(&core.Account{Address: "a", Balance: 30})

	// Revert to inner snapshot.
	s.RevertToSnapshot(snap2)
	acc, _ := s.GetAccount("a")
	if acc.Balance != 20 {
		t.Fatalf("expected 20 after inner revert, got %d", acc.Balance)
	}

	// Revert to outer snapshot.
	s.RevertToSnapshot(snap1)
	acc, _ = s.GetAccount("a")
	if acc.Balance != 10 {
		t.Fatalf("expected 10 after outer revert, got %d", acc.Balance)
	}
}

func TestStateDB_ComputeRoot_Deterministic(t *testing.T) {
	s := newTestState()
	s.SetAccount(&core.Account{Address: "a", Balance: 100})
	s.SetAccount(&core.Account{Address: "b", Balance: 200})

	r1 := s.ComputeRoot()
	r2 := s.ComputeRoot()
	if r1 != r2 {
		t.Fatal("ComputeRoot should be deterministic")
	}
}

func TestStateDB_ComputeRoot_ChangesOnMutation(t *testing.T) {
	s := newTestState()
	s.SetAccount(&core.Account{Address: "a", Balance: 100})
	r1 := s.ComputeRoot()

	s.SetAccount(&core.Account{Address: "a", Balance: 200})
	r2 := s.ComputeRoot()

	if r1 == r2 {
		t.Fatal("ComputeRoot should change after mutation")
	}
}

func TestStateDB_Commit_PersistsToDb(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	s.SetAccount(&core.Account{Address: "alice", Balance: 500})

	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}

	// After commit, a fresh StateDB on the same DB should see the data.
	s2 := storage.NewStateDB(db)
	acc, err := s2.GetAccount("alice")
	if err != nil {
		t.Fatal(err)
	}
	if acc.Balance != 500 {
		t.Fatalf("expected 500 after commit, got %d", acc.Balance)
	}
}

func TestStateDB_Session_SetAndGet(t *testing.T) {
	s := newTestState()
	sess := &core.Session{ID: "s1", GameID: "g1", Status: "open", Players: []string{"a", "b"}}
	s.SetSession(sess)

	got, err := s.GetSession("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.GameID != "g1" || got.Status != "open" {
		t.Fatal("session data mismatch")
	}
}

func TestStateDB_Inventory_DefaultEmpty(t *testing.T) {
	s := newTestState()
	inv, err := s.GetInventory("nobody")
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Slots) != 0 {
		t.Fatal("expected empty inventory")
	}
}

func TestStateDB_Listing_SetAndGet(t *testing.T) {
	s := newTestState()
	l := &core.MarketListing{ID: "l1", AssetID: "a1", Seller: "alice", Price: 100, Active: true}
	s.SetListing(l)

	got, err := s.GetListing("l1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Price != 100 || !got.Active {
		t.Fatal("listing data mismatch")
	}
}
