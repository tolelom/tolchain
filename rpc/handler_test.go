package rpc

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/internal/testutil"
)

func newTestHandler() *Handler {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	return NewHandler(bc, mempool, state, idx, "test-chain")
}

func TestGetBlockHeight_ReturnsCurrentHeight(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBlockHeight"}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	height, ok := resp.Result.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", resp.Result)
	}
	if height != 0 {
		t.Fatalf("expected height 0, got %d", height)
	}
}

func TestGetBlock_NoParams_ReturnsTip(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]any{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBlock", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	// Fresh chain has nil tip
	if resp.Result != nil {
		t.Fatalf("expected nil tip for empty chain, got %v", resp.Result)
	}
}

func TestGetBlock_ByHeight(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()

	// Add a genesis block
	_, pub, _ := crypto.GenerateKeyPair()
	block := core.NewBlock("test-chain", 0, "", pub.Hex(), nil)
	block.Hash = block.ComputeHash()
	bc.AddBlock(block)

	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	height := int64(0)
	params, _ := json.Marshal(map[string]any{"height": height})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBlock", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil block")
	}
}

func TestGetBlock_ByHash(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()

	_, pub, _ := crypto.GenerateKeyPair()
	block := core.NewBlock("test-chain", 0, "", pub.Hex(), nil)
	block.Hash = block.ComputeHash()
	bc.AddBlock(block)

	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	params, _ := json.Marshal(map[string]string{"hash": block.Hash})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBlock", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil block")
	}
}

func TestGetBalance_ReturnsAccountInfo(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	state.SetAccount(&core.Account{Address: "alice", Balance: 500, Nonce: 3})

	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	params, _ := json.Marshal(map[string]string{"address": "alice"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBalance", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", resp.Result)
	}
	if result["address"] != "alice" {
		t.Fatalf("expected address alice, got %v", result["address"])
	}
}

func TestGetBalance_MissingAddress_Error(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBalance", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing address")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Fatalf("expected CodeInvalidParams, got %d", resp.Error.Code)
	}
}

func TestSendTx_ValidTx_Succeeds(t *testing.T) {
	handler := newTestHandler()

	priv, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx, _ := core.NewTransaction("test-chain", core.TxTransfer, pubHex, 0, 0, core.TransferPayload{To: pubHex, Amount: 1})
	tx.Sign(priv)

	txJSON, _ := json.Marshal(tx)
	req := Request{JSONRPC: "2.0", ID: 1, Method: "sendTx", Params: txJSON}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestSendTx_WrongChainID_Rejected(t *testing.T) {
	handler := newTestHandler()

	priv, pub, _ := crypto.GenerateKeyPair()
	pubHex := pub.Hex()
	tx, _ := core.NewTransaction("wrong-chain", core.TxTransfer, pubHex, 0, 0, core.TransferPayload{To: pubHex, Amount: 1})
	tx.Sign(priv)

	txJSON, _ := json.Marshal(tx)
	req := Request{JSONRPC: "2.0", ID: 1, Method: "sendTx", Params: txJSON}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for wrong chain ID")
	}
}

func TestGetMempoolSize(t *testing.T) {
	handler := newTestHandler()

	req := Request{JSONRPC: "2.0", ID: 1, Method: "getMempoolSize"}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	size, ok := resp.Result.(int)
	if !ok {
		t.Fatalf("expected int, got %T", resp.Result)
	}
	if size != 0 {
		t.Fatalf("expected 0, got %d", size)
	}
}

func TestUnknownMethod_ReturnsMethodNotFound(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "nonexistentMethod"}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("expected CodeMethodNotFound (%d), got %d", CodeMethodNotFound, resp.Error.Code)
	}
}

func TestGetAsset_Valid(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	state.SetAsset(&core.Asset{ID: "a1", TemplateID: "t1", Owner: "alice"})

	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	params, _ := json.Marshal(map[string]string{"id": "a1"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getAsset", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil asset")
	}
}

func TestGetAsset_MissingID(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getAsset", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestGetSession_Valid(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	state.SetSession(&core.Session{ID: "s1", GameID: "g1", Status: "open", Players: []string{"alice"}})

	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	params, _ := json.Marshal(map[string]string{"id": "s1"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getSession", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetSession_MissingID(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getSession", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestGetListing_Valid(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	state.SetListing(&core.MarketListing{ID: "l1", AssetID: "a1", Seller: "alice", Price: 100, Active: true})

	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	params, _ := json.Marshal(map[string]string{"id": "l1"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getListing", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetListing_MissingID(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getListing", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestGetTemplate_Valid(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	state.SetTemplate(&core.AssetTemplate{ID: "t1", Name: "Sword", Creator: "alice"})

	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	params, _ := json.Marshal(map[string]string{"id": "t1"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getTemplate", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetTemplate_MissingID(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getTemplate", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestGetInventory_Valid(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{"owner": "alice"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getInventory", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetInventory_MissingOwner(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getInventory", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing owner")
	}
}

func TestGetAssetsByOwner(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	// Trigger indexer by emitting asset minted event
	emitter.Emit(events.Event{
		Type: events.EventAssetMinted,
		Data: map[string]any{"owner": "alice", "asset_id": "a1"},
	})

	params, _ := json.Marshal(map[string]any{"owner": "alice"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getAssetsByOwner", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetAssetsByOwner_Paginated(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	for i := 0; i < 5; i++ {
		emitter.Emit(events.Event{
			Type: events.EventAssetMinted,
			Data: map[string]any{"owner": "alice", "asset_id": fmt.Sprintf("a%d", i)},
		})
	}

	params, _ := json.Marshal(map[string]any{"owner": "alice", "offset": 1, "limit": 2})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getAssetsByOwner", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetAssetsByOwner_MissingOwner(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]any{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getAssetsByOwner", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing owner")
	}
}

func TestGetSessionsByPlayer(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	emitter.Emit(events.Event{
		Type: events.EventSessionOpen,
		Data: map[string]any{"session_id": "s1", "players": []any{"alice", "bob"}},
	})

	params, _ := json.Marshal(map[string]any{"player": "alice"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getSessionsByPlayer", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetSessionsByPlayer_Paginated(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	emitter.Emit(events.Event{
		Type: events.EventSessionOpen,
		Data: map[string]any{"session_id": "s1", "players": []any{"alice"}},
	})

	params, _ := json.Marshal(map[string]any{"player": "alice", "offset": 0, "limit": 5})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getSessionsByPlayer", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetSessionsByPlayer_MissingPlayer(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]any{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getSessionsByPlayer", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing player")
	}
}

func TestGetTxStatus(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	emitter.Emit(events.Event{
		Type:        events.EventTxExecuted,
		TxID:        "tx123",
		BlockHeight: 5,
		Data:        map[string]any{"success": true, "error": ""},
	})

	params, _ := json.Marshal(map[string]string{"tx_id": "tx123"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getTxStatus", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetTxStatus_MissingTxID(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getTxStatus", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing tx_id")
	}
}

func TestGetActiveListings(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	emitter.Emit(events.Event{
		Type: events.EventMarketList,
		Data: map[string]any{"listing_id": "l1"},
	})

	req := Request{JSONRPC: "2.0", ID: 1, Method: "getActiveListings"}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetActiveListings_Paginated(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	emitter.Emit(events.Event{
		Type: events.EventMarketList,
		Data: map[string]any{"listing_id": "l1"},
	})

	params, _ := json.Marshal(map[string]any{"offset": 0, "limit": 10})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getActiveListings", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetRandomCommitment_Valid(t *testing.T) {
	store := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(store)
	bc.Init()
	mempool := core.NewMempool(10000, 3600, 300)
	state := testutil.NewStateDB()
	state.SetRandomCommitment(&core.RandomCommitment{ID: "rc1", Committer: "alice", CommitHash: "abc"})
	db := testutil.NewMemDB()
	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	handler := NewHandler(bc, mempool, state, idx, "test-chain")

	params, _ := json.Marshal(map[string]string{"id": "rc1"})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getRandomCommitment", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGetRandomCommitment_MissingID(t *testing.T) {
	handler := newTestHandler()
	params, _ := json.Marshal(map[string]string{})
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getRandomCommitment", Params: params}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestGetBlock_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBlock", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetBalance_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getBalance", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetAsset_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getAsset", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetSession_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getSession", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetListing_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getListing", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetAssetsByOwner_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getAssetsByOwner", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestSendTx_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "sendTx", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetTemplate_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getTemplate", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetSessionsByPlayer_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getSessionsByPlayer", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetTxStatus_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getTxStatus", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetInventory_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getInventory", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}

func TestGetRandomCommitment_InvalidParams(t *testing.T) {
	handler := newTestHandler()
	req := Request{JSONRPC: "2.0", ID: 1, Method: "getRandomCommitment", Params: json.RawMessage(`{invalid`)}
	resp := handler.Dispatch(req)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
}
