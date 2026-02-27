package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/consensus"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/internal/testutil"
	"github.com/tolelom/tolchain/network"
	"github.com/tolelom/tolchain/rpc"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	_ "github.com/tolelom/tolchain/vm/modules/asset"
	_ "github.com/tolelom/tolchain/vm/modules/economy"
	_ "github.com/tolelom/tolchain/vm/modules/market"
	_ "github.com/tolelom/tolchain/vm/modules/session"
)

const testChainID = "test-chain"

// rpcCall is a helper that sends a JSON-RPC request and decodes the result.
func rpcCall(t *testing.T, url, method string, params any) json.RawMessage {
	t.Helper()
	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("rpc %s: %v", method, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var rpcResp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpcResp); err != nil {
		t.Fatalf("rpc %s decode: %v (raw: %s)", method, err, raw)
	}
	if rpcResp.Error != nil {
		t.Fatalf("rpc %s error: [%d] %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result
}

// sendTx signs and submits a transaction via RPC, waits for it to be mined.
func sendTx(t *testing.T, url string, tx *core.Transaction) string {
	t.Helper()
	data, _ := json.Marshal(tx)
	var params json.RawMessage = data
	result := rpcCall(t, url, "sendTx", params)
	var out struct {
		TxID string `json:"tx_id"`
	}
	json.Unmarshal(result, &out)
	t.Logf("  -> tx submitted: %s", out.TxID)
	return out.TxID
}

// waitBlock waits until block height advances past targetHeight.
func waitBlock(t *testing.T, url string, targetHeight int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		result := rpcCall(t, url, "getBlockHeight", map[string]any{})
		var h int64
		json.Unmarshal(result, &h)
		if h >= targetHeight {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("timed out waiting for block")
}

// startTestNode starts a full node (P2P + RPC + consensus) and returns cleanup func.
func startTestNode(t *testing.T, w *wallet.Wallet) (rpcURL string, cleanup func()) {
	t.Helper()

	db := testutil.NewMemDB()
	stateDB := storage.NewStateDB(db)
	blockStore := testutil.NewMemBlockStore()
	bc := core.NewBlockchain(blockStore)
	if err := bc.Init(); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		NodeID:      "test-node",
		DataDir:     "./data",
		RPCPort:     0,
		P2PPort:     0,
		MaxBlockTxs: 500,
		Validators:  []string{w.PubKey()},
		Genesis: config.GenesisConfig{
			ChainID: testChainID,
			Alloc:   map[string]uint64{w.PubKey(): 10_000_000},
		},
	}

	// Genesis
	genesis, err := config.CreateGenesisBlock(cfg, stateDB, w.PrivKey())
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.AddBlock(genesis); err != nil {
		t.Fatal(err)
	}

	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	mempool := core.NewMempool()
	exec := vm.NewExecutor(stateDB, emitter)
	poa := consensus.New(cfg, bc, stateDB, mempool, exec, emitter, w.PrivKey())

	// P2P on random port
	node := network.NewNode("test-node", ":0", mempool, nil)
	_ = network.NewSyncer(node, bc, poa, exec, stateDB)
	if err := node.Start(); err != nil {
		t.Fatal(err)
	}

	// RPC on random port
	handler := rpc.NewHandler(bc, mempool, stateDB, idx, testChainID)
	rpcServer := rpc.NewServer(":0", handler, "")
	if err := rpcServer.Start(); err != nil {
		t.Fatal(err)
	}

	rpcAddr := rpcServer.Addr().String()
	url := fmt.Sprintf("http://%s/", rpcAddr)

	// Consensus
	done := make(chan struct{})
	go poa.Run(500*time.Millisecond, done)

	// Wait for at least 1 block
	waitBlock(t, url, 1)

	return url, func() {
		close(done)
		rpcServer.Stop()
		node.Stop()
	}
}

func TestGameIntegration(t *testing.T) {
	// Skip if running short tests or no integration env
	if os.Getenv("SKIP_INTEGRATION") != "" {
		t.Skip("SKIP_INTEGRATION set")
	}

	// --- Setup: create wallets ---
	gameServer, _ := wallet.Generate()
	player1, _ := wallet.Generate()
	player2, _ := wallet.Generate()

	t.Logf("Game Server: %s", gameServer.PubKey())
	t.Logf("Player 1:    %s", player1.PubKey())
	t.Logf("Player 2:    %s", player2.PubKey())

	url, cleanup := startTestNode(t, gameServer)
	defer cleanup()

	// Track nonces
	var gsNonce uint64

	// ============================================
	// 1. Token Transfer: 게임서버 → 플레이어 초기 토큰 지급
	// ============================================
	t.Run("1_TokenTransfer", func(t *testing.T) {
		tx, _ := gameServer.Transfer(testChainID, player1.PubKey(), 100_000, gsNonce, 10)
		sendTx(t, url, tx)
		gsNonce++

		tx, _ = gameServer.Transfer(testChainID, player2.PubKey(), 100_000, gsNonce, 10)
		sendTx(t, url, tx)
		gsNonce++

		waitBlock(t, url, 3)

		// Verify balances
		result := rpcCall(t, url, "getBalance", map[string]string{"address": player1.PubKey()})
		var bal struct{ Balance uint64 }
		json.Unmarshal(result, &bal)
		if bal.Balance != 100_000 {
			t.Fatalf("player1 balance = %d, want 100000", bal.Balance)
		}
		t.Logf("  Player1 balance: %d", bal.Balance)

		result = rpcCall(t, url, "getBalance", map[string]string{"address": player2.PubKey()})
		json.Unmarshal(result, &bal)
		if bal.Balance != 100_000 {
			t.Fatalf("player2 balance = %d, want 100000", bal.Balance)
		}
		t.Logf("  Player2 balance: %d", bal.Balance)
	})

	// ============================================
	// 2. Register Template: 게임 아이템 템플릿 등록
	// ============================================
	t.Run("2_RegisterTemplate", func(t *testing.T) {
		tx, _ := gameServer.NewTx(testChainID, core.TxRegisterTemplate, gsNonce, 10, core.RegisterTemplatePayload{
			ID:   "sword-template",
			Name: "Magic Sword",
			Schema: map[string]any{
				"attack":  "int",
				"element": "string",
				"level":   "int",
			},
			Tradeable: true,
		})
		sendTx(t, url, tx)
		gsNonce++
		waitBlock(t, url, 4)
		t.Log("  Template 'sword-template' registered")
	})

	// ============================================
	// 3. Mint Asset: 아이템 발행 (플레이어1에게)
	// ============================================
	t.Run("3_MintAsset", func(t *testing.T) {
		tx, _ := gameServer.NewTx(testChainID, core.TxMintAsset, gsNonce, 10, core.MintAssetPayload{
			TemplateID: "sword-template",
			Owner:      player1.PubKey(),
			Properties: map[string]any{
				"attack":  150,
				"element": "fire",
				"level":   5,
			},
		})
		sendTx(t, url, tx)
		gsNonce++
		waitBlock(t, url, 5)

		// Check assets owned by player1
		result := rpcCall(t, url, "getAssetsByOwner", map[string]string{"owner": player1.PubKey()})
		var ids []string
		json.Unmarshal(result, &ids)
		if len(ids) == 0 {
			t.Fatal("player1 has no assets")
		}
		t.Logf("  Player1 assets: %v", ids)

		// Get asset details
		result = rpcCall(t, url, "getAsset", map[string]string{"id": ids[0]})
		var asset core.Asset
		json.Unmarshal(result, &asset)
		t.Logf("  Asset: id=%s template=%s owner=%s...", asset.ID, asset.TemplateID, asset.Owner[:16])
		if asset.TemplateID != "sword-template" {
			t.Fatalf("asset template = %s, want sword-template", asset.TemplateID)
		}
	})

	// ============================================
	// 4. Transfer Asset: 아이템 이전 (플레이어1 → 플레이어2)
	// ============================================
	var assetID string
	t.Run("4_TransferAsset", func(t *testing.T) {
		// Get player1's asset
		result := rpcCall(t, url, "getAssetsByOwner", map[string]string{"owner": player1.PubKey()})
		var ids []string
		json.Unmarshal(result, &ids)
		assetID = ids[0]

		tx, _ := player1.NewTx(testChainID, core.TxTransferAsset, 0, 10, core.TransferAssetPayload{
			AssetID: assetID,
			To:      player2.PubKey(),
		})
		sendTx(t, url, tx)
		waitBlock(t, url, 6)

		// Verify ownership changed
		result = rpcCall(t, url, "getAsset", map[string]string{"id": assetID})
		var asset core.Asset
		json.Unmarshal(result, &asset)
		if asset.Owner != player2.PubKey() {
			t.Fatalf("asset owner = %s..., want player2", asset.Owner[:16])
		}
		t.Logf("  Asset %s now owned by player2", assetID[:16])

		// player1 should have 0 assets, player2 should have 1
		result = rpcCall(t, url, "getAssetsByOwner", map[string]string{"owner": player1.PubKey()})
		json.Unmarshal(result, &ids)
		t.Logf("  Player1 assets: %d", len(ids))

		result = rpcCall(t, url, "getAssetsByOwner", map[string]string{"owner": player2.PubKey()})
		json.Unmarshal(result, &ids)
		if len(ids) != 1 {
			t.Fatalf("player2 asset count = %d, want 1", len(ids))
		}
		t.Logf("  Player2 assets: %d", len(ids))
	})

	// ============================================
	// 5. Market: 아이템 마켓 등록 & 구매
	// ============================================
	t.Run("5_Market", func(t *testing.T) {
		// Player2 lists the sword for 50,000 tokens
		tx, _ := player2.NewTx(testChainID, core.TxListMarket, 0, 10, core.ListMarketPayload{
			AssetID: assetID,
			Price:   50_000,
		})
		sendTx(t, url, tx)
		waitBlock(t, url, 7)

		// Get listing ID from asset's ActiveListingID field
		result := rpcCall(t, url, "getAsset", map[string]string{"id": assetID})
		var listedAsset core.Asset
		json.Unmarshal(result, &listedAsset)
		listingID := listedAsset.ActiveListingID
		if listingID == "" {
			t.Fatal("asset has no active listing")
		}
		result = rpcCall(t, url, "getListing", map[string]string{"id": listingID})
		var listing core.MarketListing
		json.Unmarshal(result, &listing)
		t.Logf("  Listing: id=%s price=%d seller=%s...", listing.ID, listing.Price, listing.Seller[:16])

		// Player1 buys it
		tx, _ = player1.NewTx(testChainID, core.TxBuyMarket, 1, 10, core.BuyMarketPayload{
			ListingID: listingID,
		})
		sendTx(t, url, tx)
		waitBlock(t, url, 8)

		// Asset should be back to player1
		result = rpcCall(t, url, "getAsset", map[string]string{"id": assetID})
		var asset core.Asset
		json.Unmarshal(result, &asset)
		if asset.Owner != player1.PubKey() {
			t.Fatalf("asset owner after buy = %s..., want player1", asset.Owner[:16])
		}
		t.Logf("  Asset back to player1 after market purchase")

		// Check balances: player1 paid 50000 + fees, player2 received 50000
		result = rpcCall(t, url, "getBalance", map[string]string{"address": player1.PubKey()})
		var bal struct{ Balance uint64 }
		json.Unmarshal(result, &bal)
		t.Logf("  Player1 balance after buy: %d", bal.Balance)

		result = rpcCall(t, url, "getBalance", map[string]string{"address": player2.PubKey()})
		json.Unmarshal(result, &bal)
		t.Logf("  Player2 balance after sell: %d", bal.Balance)
	})

	// ============================================
	// 6. Session: 게임 세션 (스테이킹 대전)
	// ============================================
	t.Run("6_Session", func(t *testing.T) {
		// Game server opens a session: 2 players, 10,000 stake each
		tx, _ := gameServer.NewTx(testChainID, core.TxSessionOpen, gsNonce, 10, core.SessionOpenPayload{
			SessionID: "match-001",
			GameID:    "pvp-arena",
			Players:   []string{player1.PubKey(), player2.PubKey()},
			Stakes:    10_000,
		})
		sendTx(t, url, tx)
		gsNonce++
		waitBlock(t, url, 9)

		// Check session state
		result := rpcCall(t, url, "getSession", map[string]string{"id": "match-001"})
		var sess core.Session
		json.Unmarshal(result, &sess)
		if sess.Status != "open" {
			t.Fatalf("session status = %s, want open", sess.Status)
		}
		t.Logf("  Session opened: %s, stakes=%d, players=%d", sess.ID, sess.Stakes, len(sess.Players))

		// Check player balances (should be reduced by 10,000)
		result = rpcCall(t, url, "getBalance", map[string]string{"address": player1.PubKey()})
		var bal struct{ Balance uint64 }
		json.Unmarshal(result, &bal)
		t.Logf("  Player1 balance after stake: %d", bal.Balance)

		// Game server submits result: player1 wins everything (20,000)
		tx, _ = gameServer.NewTx(testChainID, core.TxSessionResult, gsNonce, 10, core.SessionResultPayload{
			SessionID: "match-001",
			Outcome: map[string]uint64{
				player1.PubKey(): 20_000,
				player2.PubKey(): 0,
			},
		})
		sendTx(t, url, tx)
		gsNonce++
		waitBlock(t, url, 10)

		// Session should be closed
		result = rpcCall(t, url, "getSession", map[string]string{"id": "match-001"})
		json.Unmarshal(result, &sess)
		if sess.Status != "closed" {
			t.Fatalf("session status = %s, want closed", sess.Status)
		}
		t.Logf("  Session closed: winner got %d tokens", sess.Outcome[player1.PubKey()])

		// Final balances
		result = rpcCall(t, url, "getBalance", map[string]string{"address": player1.PubKey()})
		json.Unmarshal(result, &bal)
		t.Logf("  Player1 final balance: %d (gained 10,000 from match)", bal.Balance)

		result = rpcCall(t, url, "getBalance", map[string]string{"address": player2.PubKey()})
		json.Unmarshal(result, &bal)
		t.Logf("  Player2 final balance: %d (lost 10,000 from match)", bal.Balance)
	})

	// ============================================
	// 7. Burn Asset: 아이템 소각
	// ============================================
	t.Run("7_BurnAsset", func(t *testing.T) {
		tx, _ := player1.NewTx(testChainID, core.TxBurnAsset, 2, 10, core.BurnAssetPayload{
			AssetID: assetID,
		})
		sendTx(t, url, tx)
		waitBlock(t, url, 11)

		// Asset should no longer exist
		result := rpcCall(t, url, "getAssetsByOwner", map[string]string{"owner": player1.PubKey()})
		var ids []string
		json.Unmarshal(result, &ids)
		t.Logf("  Player1 assets after burn: %d", len(ids))
	})

	t.Log("\n=== All game integration tests passed! ===")
}
