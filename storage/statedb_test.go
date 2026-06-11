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

func newTestStateDB(t *testing.T) *storage.StateDB {
	t.Helper()
	return storage.NewStateDB(testutil.NewMemDB())
}

// --- Account ---

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

func TestSetAccountOverwrite(t *testing.T) {
	state := newTestStateDB(t)

	addr := "overwrite-addr"
	if err := state.SetAccount(&core.Account{Address: addr, Balance: 100}); err != nil {
		t.Fatal(err)
	}
	if err := state.SetAccount(&core.Account{Address: addr, Balance: 999}); err != nil {
		t.Fatal(err)
	}

	acc, _ := state.GetAccount(addr)
	if acc.Balance != 999 {
		t.Errorf("expected 999, got %d", acc.Balance)
	}
}

// --- Asset ---

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

// --- Snapshot and Rollback ---

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

func TestRevertToSnapshot_NegativeID(t *testing.T) {
	state := newTestStateDB(t)

	if err := state.RevertToSnapshot(-1); err == nil {
		t.Error("expected error for negative snapshot id")
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

// --- ComputeRoot ---

func TestStateDB_ComputeRoot_Deterministic(t *testing.T) {
	s := newTestState()
	s.SetAccount(&core.Account{Address: "a", Balance: 100})
	s.SetAccount(&core.Account{Address: "b", Balance: 200})

	r1, err := s.ComputeRoot()
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.ComputeRoot()
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Fatal("ComputeRoot should be deterministic")
	}
}

func TestStateDB_ComputeRoot_ChangesOnMutation(t *testing.T) {
	s := newTestState()
	s.SetAccount(&core.Account{Address: "a", Balance: 100})
	r1, err := s.ComputeRoot()
	if err != nil {
		t.Fatal(err)
	}

	s.SetAccount(&core.Account{Address: "a", Balance: 200})
	r2, err := s.ComputeRoot()
	if err != nil {
		t.Fatal(err)
	}

	if r1 == r2 {
		t.Fatal("ComputeRoot should change after mutation")
	}
}

func TestComputeRoot_DifferentStatesDifferentRoots(t *testing.T) {
	state1 := newTestStateDB(t)
	state2 := newTestStateDB(t)

	_ = state1.SetAccount(&core.Account{Address: "alice", Balance: 100})
	_ = state2.SetAccount(&core.Account{Address: "alice", Balance: 200})

	root1, err := state1.ComputeRoot()
	if err != nil {
		t.Fatal(err)
	}
	root2, err := state2.ComputeRoot()
	if err != nil {
		t.Fatal(err)
	}

	if root1 == root2 {
		t.Error("different balances should produce different roots")
	}
}

// --- Commit ---

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

func TestCommit_ClearsDirtyAndSnapshots(t *testing.T) {
	state := newTestStateDB(t)

	_ = state.SetAccount(&core.Account{Address: "x", Balance: 1})
	snapID, _ := state.Snapshot()

	if err := state.Commit(); err != nil {
		t.Fatal(err)
	}

	// After commit, old snapshot should be invalid.
	if err := state.RevertToSnapshot(snapID); err == nil {
		t.Error("snapshot should be invalidated after commit")
	}
}

func TestCommit_DeletedKeysRemovedFromDB(t *testing.T) {
	state := newTestStateDB(t)

	// Set and commit an asset, then delete and commit again.
	asset := &core.Asset{ID: "del-test", Owner: "alice", TemplateID: "tmpl1"}
	_ = state.SetAsset(asset)
	_ = state.Commit()

	_ = state.DeleteAsset("del-test")
	_ = state.Commit()

	_, err := state.GetAsset("del-test")
	if err == nil {
		t.Error("deleted asset should not be retrievable after commit")
	}
}

// --- Session ---

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

// --- Inventory ---

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

func TestStateDB_Inventory_SetAndGet(t *testing.T) {
	s := newTestState()
	inv := &core.Inventory{Owner: "alice", Slots: map[string]string{"weapon": "sword1", "armor": "plate1"}}
	if err := s.SetInventory(inv); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetInventory("alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Slots["weapon"] != "sword1" || got.Slots["armor"] != "plate1" {
		t.Fatal("inventory data mismatch")
	}
}

func TestStateDB_Inventory_SetOverwrite(t *testing.T) {
	s := newTestState()
	inv1 := &core.Inventory{Owner: "alice", Slots: map[string]string{"weapon": "sword1"}}
	if err := s.SetInventory(inv1); err != nil {
		t.Fatal(err)
	}

	inv2 := &core.Inventory{Owner: "alice", Slots: map[string]string{"weapon": "sword2", "shield": "buckler"}}
	if err := s.SetInventory(inv2); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetInventory("alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Slots["weapon"] != "sword2" {
		t.Fatalf("expected sword2, got %s", got.Slots["weapon"])
	}
	if got.Slots["shield"] != "buckler" {
		t.Fatalf("expected buckler, got %s", got.Slots["shield"])
	}
}

// --- Listing ---

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

// --- Template ---

func TestStateDB_Template_SetAndGet(t *testing.T) {
	s := newTestState()
	tmpl := &core.AssetTemplate{ID: "t1", Name: "Sword", Creator: "alice"}
	if err := s.SetTemplate(tmpl); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTemplate("t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "t1" || got.Name != "Sword" || got.Creator != "alice" {
		t.Fatalf("template data mismatch: got %+v", got)
	}
}

func TestStateDB_Template_NotFound(t *testing.T) {
	s := newTestState()
	_, err := s.GetTemplate("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}

// --- RandomCommitment ---

func TestStateDB_RandomCommitment_SetAndGet(t *testing.T) {
	s := newTestState()
	rc := &core.RandomCommitment{ID: "rc1", Committer: "alice", CommitHash: "abc123"}
	if err := s.SetRandomCommitment(rc); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetRandomCommitment("rc1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "rc1" || got.Committer != "alice" || got.CommitHash != "abc123" {
		t.Fatalf("random commitment data mismatch: got %+v", got)
	}
}

func TestStateDB_RandomCommitment_NotFound(t *testing.T) {
	s := newTestState()
	_, err := s.GetRandomCommitment("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent random commitment")
	}
}

// ---- Corrupt data tests (cover json.Unmarshal error branches) ----

func TestStateDB_GetAccount_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	db.Set([]byte("acct:alice"), []byte("not json"))
	_, err := s.GetAccount("alice")
	if err == nil {
		t.Fatal("expected error for corrupt account data")
	}
}

func TestStateDB_GetAsset_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	db.Set([]byte("asset:a1"), []byte("not json"))
	_, err := s.GetAsset("a1")
	if err == nil {
		t.Fatal("expected error for corrupt asset data")
	}
}

func TestStateDB_GetTemplate_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	db.Set([]byte("tmpl:t1"), []byte("not json"))
	_, err := s.GetTemplate("t1")
	if err == nil {
		t.Fatal("expected error for corrupt template data")
	}
}

func TestStateDB_GetSession_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	db.Set([]byte("sess:s1"), []byte("not json"))
	_, err := s.GetSession("s1")
	if err == nil {
		t.Fatal("expected error for corrupt session data")
	}
}

func TestStateDB_GetListing_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	db.Set([]byte("list:l1"), []byte("not json"))
	_, err := s.GetListing("l1")
	if err == nil {
		t.Fatal("expected error for corrupt listing data")
	}
}

func TestStateDB_GetInventory_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	db.Set([]byte("inv:alice"), []byte("not json"))
	_, err := s.GetInventory("alice")
	if err == nil {
		t.Fatal("expected error for corrupt inventory data")
	}
}

func TestStateDB_GetRandomCommitment_CorruptData(t *testing.T) {
	db := testutil.NewMemDB()
	s := storage.NewStateDB(db)
	db.Set([]byte("rcom:rc1"), []byte("not json"))
	_, err := s.GetRandomCommitment("rc1")
	if err == nil {
		t.Fatal("expected error for corrupt random commitment data")
	}
}
