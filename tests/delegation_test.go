package tests

import (
	"testing"
	"time"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	_ "github.com/tolelom/tolchain/vm/modules/delegation"
	// economy, asset etc. already registered by other test files in this package
)

const delegationChainID = "test-chain"

// setupDelegationTest creates an in-memory state, executor, and two funded wallets.
func setupDelegationTest(t *testing.T) (state core.State, exec *vm.Executor, granter, grantee *wallet.Wallet) {
	t.Helper()
	state = storage.NewStateDB(testutil.NewMemDB())
	emitter := events.NewEmitter()
	exec = vm.NewExecutor(state, emitter)
	granter, _ = wallet.Generate()
	grantee, _ = wallet.Generate()
	// Fund granter; grantee doesn't need balance (fees come from granter)
	_ = state.SetAccount(&core.Account{Address: granter.PubKey(), Balance: 1_000_000})
	return
}

// execTx wraps a single transaction in a minimal block and executes it.
func execTx(t *testing.T, exec *vm.Executor, tx *core.Transaction, height int64, validator string) error {
	t.Helper()
	block := core.NewBlock(delegationChainID, height, "0000", validator, []*core.Transaction{tx})
	return exec.ExecuteTx(block, tx)
}

// TestDelegation_E2E tests the full grant → delegated-transfer → revoke → rejected flow.
func TestDelegation_E2E(t *testing.T) {
	state, exec, granter, grantee := setupDelegationTest(t)
	receiver, _ := wallet.Generate()

	// Step 1: granter issues grant_delegation (nonce=0)
	grantTx, err := granter.GrantDelegation(
		delegationChainID,
		grantee.PubKey(),
		[]string{"transfer"},
		0, // no expiry
		0, // unlimited uses
		0, // no max amount
		0, // nonce
		10,
	)
	if err != nil {
		t.Fatalf("GrantDelegation: %v", err)
	}
	if err := execTx(t, exec, grantTx, 1, granter.PubKey()); err != nil {
		t.Fatalf("grant_delegation tx: %v", err)
	}

	// Verify grant exists in state
	grant, err := state.GetDelegation(granter.PubKey(), grantee.PubKey())
	if err != nil {
		t.Fatalf("GetDelegation after grant: %v", err)
	}
	if grant.Grantee != grantee.PubKey() {
		t.Errorf("grant.Grantee = %s, want %s", grant.Grantee, grantee.PubKey())
	}

	// Step 2: grantee sends a delegated transfer on behalf of granter (granter's nonce=1)
	granterBalanceBefore, _ := state.GetAccount(granter.PubKey())
	transferAmount := uint64(5000)
	delegatedTx, err := grantee.NewDelegatedTx(
		delegationChainID,
		core.TxTransfer,
		granter.PubKey(),
		1, // granter's nonce
		10,
		core.TransferPayload{To: receiver.PubKey(), Amount: transferAmount},
	)
	if err != nil {
		t.Fatalf("NewDelegatedTx: %v", err)
	}
	if err := execTx(t, exec, delegatedTx, 2, granter.PubKey()); err != nil {
		t.Fatalf("delegated transfer tx: %v", err)
	}

	// Verify balances
	granterAcc, _ := state.GetAccount(granter.PubKey())
	receiverAcc, _ := state.GetAccount(receiver.PubKey())
	granteeAcc, _ := state.GetAccount(grantee.PubKey())

	// granter is the effectiveSender and also the block proposer, so the fee (10) is credited
	// back to granter. Net: granter loses only the transfer amount (5000).
	expectedGranterBalance := granterBalanceBefore.Balance - transferAmount
	if granterAcc.Balance != expectedGranterBalance {
		t.Errorf("granter balance = %d, want %d", granterAcc.Balance, expectedGranterBalance)
	}
	if receiverAcc.Balance != transferAmount {
		t.Errorf("receiver balance = %d, want %d", receiverAcc.Balance, transferAmount)
	}
	if granteeAcc.Balance != 0 {
		t.Errorf("grantee balance should be 0, got %d", granteeAcc.Balance)
	}

	// Step 3: granter revokes delegation (granter's nonce=2)
	revokeTx, err := granter.RevokeDelegation(delegationChainID, grantee.PubKey(), 2, 10)
	if err != nil {
		t.Fatalf("RevokeDelegation: %v", err)
	}
	if err := execTx(t, exec, revokeTx, 3, granter.PubKey()); err != nil {
		t.Fatalf("revoke_delegation tx: %v", err)
	}

	// Verify grant deleted from state
	if _, err := state.GetDelegation(granter.PubKey(), grantee.PubKey()); err == nil {
		t.Error("delegation grant should not exist after revocation")
	}

	// Step 4: another delegated transfer must fail after revocation (granter's nonce=3)
	delegatedTx2, err := grantee.NewDelegatedTx(
		delegationChainID,
		core.TxTransfer,
		granter.PubKey(),
		3,
		10,
		core.TransferPayload{To: receiver.PubKey(), Amount: 100},
	)
	if err != nil {
		t.Fatalf("NewDelegatedTx (after revoke): %v", err)
	}
	if err := execTx(t, exec, delegatedTx2, 4, granter.PubKey()); err == nil {
		t.Error("delegated tx after revocation should fail")
	}
}

// TestDelegation_E2E_MaxUses verifies that a grant with max_uses=2 rejects the 3rd attempt.
func TestDelegation_E2E_MaxUses(t *testing.T) {
	state, exec, granter, grantee := setupDelegationTest(t)
	receiver, _ := wallet.Generate()

	// Grant with max_uses=2 (nonce=0)
	grantTx, err := granter.GrantDelegation(
		delegationChainID,
		grantee.PubKey(),
		[]string{"transfer"},
		0,
		2, // max_uses
		0,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("GrantDelegation: %v", err)
	}
	if err := execTx(t, exec, grantTx, 1, granter.PubKey()); err != nil {
		t.Fatalf("grant_delegation: %v", err)
	}

	// First delegated TX (granter nonce=1) — must succeed
	for i := 0; i < 2; i++ {
		tx, err := grantee.NewDelegatedTx(
			delegationChainID,
			core.TxTransfer,
			granter.PubKey(),
			uint64(1+i),
			0,
			core.TransferPayload{To: receiver.PubKey(), Amount: 100},
		)
		if err != nil {
			t.Fatalf("NewDelegatedTx[%d]: %v", i, err)
		}
		if err := execTx(t, exec, tx, int64(2+i), granter.PubKey()); err != nil {
			t.Fatalf("delegated tx[%d] should succeed: %v", i, err)
		}
	}

	// Verify used count
	grant, _ := state.GetDelegation(granter.PubKey(), grantee.PubKey())
	if grant.UsedCount != 2 {
		t.Errorf("UsedCount = %d, want 2", grant.UsedCount)
	}

	// Third TX (granter nonce=3) — must fail
	tx3, err := grantee.NewDelegatedTx(
		delegationChainID,
		core.TxTransfer,
		granter.PubKey(),
		3,
		0,
		core.TransferPayload{To: receiver.PubKey(), Amount: 100},
	)
	if err != nil {
		t.Fatalf("NewDelegatedTx[2]: %v", err)
	}
	if err := execTx(t, exec, tx3, 4, granter.PubKey()); err == nil {
		t.Error("3rd delegated tx should fail (max_uses=2 exceeded)")
	}
}

// TestDelegation_E2E_Expiry verifies that an already-expired delegation is rejected.
func TestDelegation_E2E_Expiry(t *testing.T) {
	_, exec, granter, grantee := setupDelegationTest(t)
	receiver, _ := wallet.Generate()

	// Grant with expiry in the past (nonce=0)
	pastExpiry := time.Now().Unix() - 3600
	grantTx, err := granter.GrantDelegation(
		delegationChainID,
		grantee.PubKey(),
		[]string{"transfer"},
		pastExpiry,
		0,
		0,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("GrantDelegation: %v", err)
	}
	if err := execTx(t, exec, grantTx, 1, granter.PubKey()); err != nil {
		t.Fatalf("grant_delegation: %v", err)
	}

	// Attempt delegated TX with expired grant (granter nonce=1) — must fail
	delegatedTx, err := grantee.NewDelegatedTx(
		delegationChainID,
		core.TxTransfer,
		granter.PubKey(),
		1,
		0,
		core.TransferPayload{To: receiver.PubKey(), Amount: 100},
	)
	if err != nil {
		t.Fatalf("NewDelegatedTx: %v", err)
	}
	if err := execTx(t, exec, delegatedTx, 2, granter.PubKey()); err == nil {
		t.Error("delegated tx with expired grant should fail")
	}
}

// TestDelegation_ExpiryDeterministic verifies that delegation expiry is
// evaluated against the executing block's timestamp, not the local wall clock.
// A grant already expired in wall-clock time must still be usable inside a
// block whose timestamp predates the expiry — otherwise replay/sync of old
// blocks is non-deterministic across nodes.
func TestDelegation_ExpiryDeterministic(t *testing.T) {
	state, exec, granter, grantee := setupDelegationTest(t)
	receiver, _ := wallet.Generate()

	expiresAt := time.Now().Unix() - 3600 // expired 1h ago in wall-clock time

	// Install the grant directly in state, as if granted in an old block.
	if err := state.SetDelegation(&core.DelegationGrant{
		Granter:    granter.PubKey(),
		Grantee:    grantee.PubKey(),
		AllowTypes: []string{"transfer"},
		ExpiresAt:  expiresAt,
	}); err != nil {
		t.Fatal(err)
	}

	tx, err := grantee.NewDelegatedTx(
		delegationChainID,
		core.TxTransfer,
		granter.PubKey(),
		0,
		0,
		core.TransferPayload{To: receiver.PubKey(), Amount: 100},
	)
	if err != nil {
		t.Fatalf("NewDelegatedTx: %v", err)
	}

	// Block timestamped 1h BEFORE the expiry: at block time the grant is valid.
	block := core.NewBlock(delegationChainID, 2, "0000", granter.PubKey(), []*core.Transaction{tx})
	block.Header.Timestamp = (expiresAt - 3600) * int64(time.Second)

	if err := exec.ExecuteTx(block, tx); err != nil {
		t.Fatalf("delegated tx must succeed at block time before expiry (wall clock must not matter), got: %v", err)
	}
}

// TestDelegation_E2E_TypeNotAllowed verifies that a grantee cannot use a type outside allow_types.
func TestDelegation_E2E_TypeNotAllowed(t *testing.T) {
	_, exec, granter, grantee := setupDelegationTest(t)

	// Grant only "transfer" (nonce=0)
	grantTx, err := granter.GrantDelegation(
		delegationChainID,
		grantee.PubKey(),
		[]string{"transfer"},
		0,
		0,
		0,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("GrantDelegation: %v", err)
	}
	if err := execTx(t, exec, grantTx, 1, granter.PubKey()); err != nil {
		t.Fatalf("grant_delegation: %v", err)
	}

	// Try a delegated "mint_asset" TX (granter nonce=1) — must fail
	delegatedTx, err := grantee.NewDelegatedTx(
		delegationChainID,
		core.TxMintAsset,
		granter.PubKey(),
		1,
		0,
		core.MintAssetPayload{
			TemplateID: "sword-template",
			Owner:      grantee.PubKey(),
			Properties: map[string]any{"attack": 10},
		},
	)
	if err != nil {
		t.Fatalf("NewDelegatedTx: %v", err)
	}
	if err := execTx(t, exec, delegatedTx, 2, granter.PubKey()); err == nil {
		t.Error("delegated mint_asset should fail: type not in allow_types")
	}
}

// TestDelegation_E2E_PreventReDelegation verifies that a grantee cannot re-delegate on behalf of the granter.
func TestDelegation_E2E_PreventReDelegation(t *testing.T) {
	_, exec, granter, grantee := setupDelegationTest(t)
	thirdParty, _ := wallet.Generate()

	// Grant "transfer" permission (nonce=0)
	grantTx, err := granter.GrantDelegation(
		delegationChainID,
		grantee.PubKey(),
		[]string{"transfer"},
		0,
		0,
		0,
		0,
		10,
	)
	if err != nil {
		t.Fatalf("GrantDelegation: %v", err)
	}
	if err := execTx(t, exec, grantTx, 1, granter.PubKey()); err != nil {
		t.Fatalf("grant_delegation: %v", err)
	}

	// Grantee tries to issue a grant_delegation TX on behalf of granter (granter nonce=1) — must fail
	reDelegateTx, err := grantee.NewDelegatedTx(
		delegationChainID,
		core.TxGrantDelegation,
		granter.PubKey(),
		1,
		0,
		core.GrantDelegationPayload{
			Grantee:    thirdParty.PubKey(),
			AllowTypes: []string{"transfer"},
		},
	)
	if err != nil {
		t.Fatalf("NewDelegatedTx (re-delegation): %v", err)
	}
	if err := execTx(t, exec, reDelegateTx, 2, granter.PubKey()); err == nil {
		t.Error("re-delegation via delegated TX should fail")
	}
}
