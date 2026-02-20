package tests

import (
	"encoding/json"
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/rpc"
	"github.com/tolelom/tolchain/storage"
)

// newTestRPCHandler builds an RPC handler backed by in-memory state.
func newTestRPCHandler(t *testing.T) *rpc.Handler {
	t.Helper()
	db := testutil.NewMemDB()
	state := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	mp := core.NewMempool()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	return rpc.NewHandler(bc, mp, state, idx)
}

func dispatch(handler *rpc.Handler, method string, params any) rpc.Response {
	raw, _ := json.Marshal(params)
	return handler.Dispatch(rpc.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  raw,
	})
}

// TestRPCGetBlockHeight verifies that getBlockHeight returns 0 for a fresh chain.
func TestRPCGetBlockHeight(t *testing.T) {
	handler := newTestRPCHandler(t)
	resp := dispatch(handler, "getBlockHeight", struct{}{})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	// Dispatch is called directly (no HTTP round-trip), so result is int64, not float64.
	var height int64
	switch v := resp.Result.(type) {
	case int64:
		height = v
	case float64:
		height = int64(v)
	default:
		t.Fatalf("unexpected result type %T", resp.Result)
	}
	if height != 0 {
		t.Errorf("height: got %d want 0", height)
	}
}

// TestRPCGetBalance verifies getBalance returns zero for an unknown account.
func TestRPCGetBalance(t *testing.T) {
	handler := newTestRPCHandler(t)
	resp := dispatch(handler, "getBalance", map[string]string{"address": "nonexistent"})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	balance, _ := result["balance"].(float64)
	if balance != 0 {
		t.Errorf("balance: got %v want 0", balance)
	}
}

// TestRPCGetMempoolSize verifies getMempoolSize returns 0 for an empty mempool.
func TestRPCGetMempoolSize(t *testing.T) {
	handler := newTestRPCHandler(t)
	resp := dispatch(handler, "getMempoolSize", struct{}{})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	size, _ := resp.Result.(float64)
	if int(size) != 0 {
		t.Errorf("mempool size: got %d want 0", int(size))
	}
}

// TestRPCMethodNotFound verifies that unknown methods return a -32601 error.
func TestRPCMethodNotFound(t *testing.T) {
	handler := newTestRPCHandler(t)
	resp := dispatch(handler, "nonExistentMethod", struct{}{})
	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	if resp.Error.Code != rpc.CodeMethodNotFound {
		t.Errorf("error code: got %d want %d", resp.Error.Code, rpc.CodeMethodNotFound)
	}
}
