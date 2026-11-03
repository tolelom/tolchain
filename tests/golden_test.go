package tests

import (
	"crypto/ed25519"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/network"
	"github.com/tolelom/tolchain/rpc"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
)

// The golden file pins every byte that other nodes, the disk, or clients
// depend on. A refactor must leave it unchanged; regenerate with -update only
// for an intended, chain-breaking format change.
var updateGolden = flag.Bool("update", false, "rewrite tests/testdata golden files")

const (
	goldenChainID = "golden-chain"
	goldenT0      = int64(1_767_225_600_000_000_000) // 2026-01-01T00:00:00Z
)

type goldenKey struct{ priv crypto.PrivateKey }

func newGoldenKey(seed byte) goldenKey {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed
	}
	return goldenKey{priv: crypto.PrivateKey(ed25519.NewKeyFromSeed(s))}
}

func (k goldenKey) pub() string { return k.priv.Public().Hex() }

type goldenChain struct {
	t        *testing.T
	db       *storage.LevelDB
	state    *storage.StateDB
	bc       *core.Blockchain
	exec     *vm.Executor
	emitter  *events.Emitter
	idx      *indexer.Indexer
	nonces   map[string]uint64
	txSeq    int64
	out      *strings.Builder
	blocks   []*core.Block
	operator goldenKey
	proposer goldenKey
}

func newGoldenChain(t *testing.T, dir string, operator, proposer goldenKey, alloc map[string]uint64) *goldenChain {
	t.Helper()
	db, err := storage.NewLevelDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	g := &goldenChain{
		t: t, db: db, state: storage.NewStateDB(db), emitter: events.NewEmitter(),
		nonces: map[string]uint64{}, out: &strings.Builder{}, operator: operator, proposer: proposer,
	}
	g.bc = core.NewBlockchain(storage.NewLevelBlockStore(db))
	if err := g.bc.Init(); err != nil {
		t.Fatal(err)
	}
	g.idx = indexer.New(db, g.emitter)
	for _, typ := range events.EventTypeValues() {
		g.emitter.Subscribe(typ, func(ev events.Event) {
			data, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			fmt.Fprintf(g.out, "event %s\n", data)
		})
	}

	cfg := config.DefaultConfig()
	cfg.Genesis = config.GenesisConfig{ChainID: goldenChainID, Alloc: alloc, Timestamp: goldenT0}
	cfg.MaxTotalSupply = 10_000_000
	genesis, err := config.CreateGenesisBlock(cfg, g.state)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.bc.AddBlock(genesis); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(g.out, "genesis hash=%s state_root=%s\n", genesis.Hash, genesis.Header.StateRoot)
	g.blocks = append(g.blocks, genesis)

	g.exec = vm.NewExecutor(g.state, g.emitter)
	g.exec.SetOperators([]string{operator.pub()})
	g.exec.SetMaxTotalSupply(cfg.MaxTotalSupply)
	g.exec.SetBlockHashes(func(h int64) (string, error) {
		b, err := g.bc.GetBlockByHeight(h)
		if err != nil {
			return "", err
		}
		return b.Hash, nil
	})
	return g
}

// tx builds a signed transaction with a fixed timestamp and the next nonce of
// the account it spends from.
func (g *goldenChain) tx(signer goldenKey, onBehalfOf string, typ core.TxType, fee uint64, payload any) *core.Transaction {
	raw, err := json.Marshal(payload)
	if err != nil {
		g.t.Fatal(err)
	}
	account := signer.pub()
	if onBehalfOf != "" {
		account = onBehalfOf
	}
	g.txSeq++
	tx := &core.Transaction{
		ChainID: goldenChainID, Type: typ, From: signer.pub(), Nonce: g.nonces[account],
		Fee: fee, Timestamp: goldenT0 + g.txSeq*int64(time.Millisecond), Payload: raw, OnBehalfOf: onBehalfOf,
	}
	tx.Sign(signer.priv)
	g.nonces[account]++
	return tx
}

// failing builds a tx expected to fail; failed txs do not consume a nonce.
func (g *goldenChain) failing(tx *core.Transaction) *core.Transaction {
	g.nonces[tx.NonceAccount()]--
	return tx
}

// rawTx is like tx but takes the payload bytes verbatim.
func (g *goldenChain) rawTx(signer goldenKey, typ core.TxType, payload string) *core.Transaction {
	g.txSeq++
	tx := &core.Transaction{
		ChainID: goldenChainID, Type: typ, From: signer.pub(), Nonce: g.nonces[signer.pub()],
		Timestamp: goldenT0 + g.txSeq*int64(time.Millisecond), Payload: json.RawMessage(payload),
	}
	tx.Sign(signer.priv)
	g.nonces[signer.pub()]++
	return tx
}

// block executes txs the way the producer does (failures are skipped and
// reported), then signs, stores and commits the block.
func (g *goldenChain) block(txs ...*core.Transaction) *core.Block {
	tip := g.bc.Tip()
	height := tip.Header.Height + 1
	b := core.NewBlock(goldenChainID, height, tip.Hash, g.proposer.pub(), txs)
	b.Header.Timestamp = goldenT0 + height*int64(2*time.Second)

	buf := events.NewBuffer()
	g.exec.SetEmitter(buf)
	var ok []*core.Transaction
	for _, tx := range txs {
		if err := g.exec.ExecuteTx(b, tx); err != nil {
			fmt.Fprintf(g.out, "tx_failed height=%d type=%s err=%s\n", height, tx.Type, err)
			continue
		}
		ok = append(ok, tx)
	}
	g.exec.SetEmitter(g.emitter)

	b.Transactions = ok
	b.Header.TxRoot = core.ComputeTxRoot(ok)
	root, err := g.state.ComputeRoot()
	if err != nil {
		g.t.Fatal(err)
	}
	b.Header.StateRoot = root
	b.Sign(g.proposer.priv)
	if err := g.bc.AddBlock(b); err != nil {
		g.t.Fatal(err)
	}
	if err := g.state.Commit(); err != nil {
		g.t.Fatal(err)
	}
	buf.Flush(g.emitter)
	fmt.Fprintf(g.out, "block height=%d txs=%d hash=%s state_root=%s tx_root=%s\n",
		height, len(ok), b.Hash, root, b.Header.TxRoot)
	g.blocks = append(g.blocks, b)
	return b
}

func (g *goldenChain) dumpDB() {
	it := g.db.NewIterator(nil)
	defer it.Release()
	var lines []string
	for it.Next() {
		lines = append(lines, fmt.Sprintf("kv %q = %q", it.Key(), it.Value()))
	}
	if err := it.Error(); err != nil {
		g.t.Fatal(err)
	}
	sort.Strings(lines)
	for _, l := range lines {
		g.out.WriteString(l + "\n")
	}
}

func (g *goldenChain) rpc(method string, params string) {
	h := rpc.NewHandler(g.bc, core.NewMempool(100, 3600, 300), g.state, g.idx, goldenChainID)
	var raw json.RawMessage
	if params != "" {
		raw = json.RawMessage(params)
	}
	resp := h.Dispatch(rpc.Request{JSONRPC: "2.0", ID: 7, Method: method, Params: raw})
	data, err := json.Marshal(resp)
	if err != nil {
		g.t.Fatal(err)
	}
	fmt.Fprintf(g.out, "rpc %s %s -> %s\n", method, params, data)
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if string(want) == got {
		return
	}
	wantLines, gotLines := strings.Split(string(want), "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			t.Fatalf("%s differs at line %d\nwant: %s\ngot:  %s", name, i+1, w, g)
		}
	}
}

// TestGoldenChain drives every tx type through block execution on LevelDB and
// pins block/tx hashes, state roots, the on-disk key/value bytes, event
// payloads (indexer and SSE input) and RPC responses.
func TestGoldenChain(t *testing.T) {
	op, prop := newGoldenKey(1), newGoldenKey(2)
	alice, bob, carol, dave := newGoldenKey(3), newGoldenKey(4), newGoldenKey(5), newGoldenKey(6)
	g := newGoldenChain(t, t.TempDir(), op, prop, map[string]uint64{op.pub(): 1_000_000, alice.pub(): 5_000})

	tmplTx := g.tx(op, "", core.TxRegisterTemplate, 0, core.RegisterTemplatePayload{
		ID: "sword", Name: "Sword", Schema: map[string]any{"atk": "int", "name": "string"}, Tradeable: true})
	mintTx := g.tx(op, "", core.TxMintAsset, 0, core.MintAssetPayload{
		TemplateID: "sword", Owner: bob.pub(), Properties: map[string]any{"atk": 5, "name": "excalibur"}})
	commitTx := g.tx(op, "", core.TxRandomCommit, 0, core.RandomCommitPayload{CommitID: "r1", CommitHash: crypto.Hash([]byte("s3cret"))})
	g.block(
		tmplTx,
		mintTx,
		commitTx,
		g.tx(op, "", core.TxTransfer, 1, core.TransferPayload{To: bob.pub(), Amount: 1_000}),
		g.tx(op, "", core.TxTransfer, 1, core.TransferPayload{To: carol.pub(), Amount: 500}),
		g.failing(g.rawTx(alice, core.TxTransfer, `{"to":"x","amount":"bad"}`)),
		g.failing(g.tx(alice, "", core.TxMintAsset, 0, core.MintAssetPayload{TemplateID: "sword"})),
	)
	swordID := crypto.Hash([]byte(mintTx.ID + ":asset:sword"))

	rewardTx := g.tx(op, "", core.TxGrantReward, 0, core.GrantRewardPayload{
		Recipient: carol.pub(), TokenAmount: 50,
		Assets: []core.MintAssetPayload{{TemplateID: "sword", Properties: map[string]any{"atk": 1}}}})
	listTx := g.tx(bob, "", core.TxListMarket, 0, core.ListMarketPayload{AssetID: swordID, Price: 100})
	consent := core.SessionConsentMessage(goldenChainID, "s1", "arena", 10)
	g.block(
		rewardTx,
		listTx,
		g.tx(op, "", core.TxSessionOpen, 0, core.SessionOpenPayload{
			SessionID: "s1", GameID: "arena", Players: []string{bob.pub(), carol.pub()}, Stakes: 10,
			Signatures: map[string]string{
				bob.pub():   crypto.Sign(bob.priv, consent),
				carol.pub(): crypto.Sign(carol.priv, consent),
			}}),
		g.tx(bob, "", core.TxGrantDelegation, 0, core.GrantDelegationPayload{
			Grantee: dave.pub(), AllowTypes: []string{string(core.TxTransfer), string(core.TxEquipItem)},
			ExpiresAt: goldenT0/int64(time.Second) + 1_000, MaxUses: 5, MaxAmount: 100}),
		g.failing(g.tx(bob, "", core.TxSessionOpen, 0, core.SessionOpenPayload{SessionID: "s9", Players: []string{bob.pub()}})),
	)
	rewardSwordID := crypto.Hash([]byte(rewardTx.ID + ":reward:0"))
	listingID := crypto.Hash([]byte(listTx.ID + ":listing:" + swordID))

	g.block(
		g.tx(carol, "", core.TxBuyMarket, 0, core.BuyMarketPayload{ListingID: listingID}),
		g.tx(carol, "", core.TxEquipItem, 0, core.EquipItemPayload{AssetID: swordID, Slot: "hand"}),
		g.tx(op, "", core.TxSessionResult, 0, core.SessionResultPayload{
			SessionID: "s1", Outcome: map[string]uint64{bob.pub(): 15, carol.pub(): 5}}),
		g.tx(dave, bob.pub(), core.TxTransfer, 0, core.TransferPayload{To: carol.pub(), Amount: 30}),
		g.failing(g.tx(dave, bob.pub(), core.TxTransfer, 0, core.TransferPayload{To: carol.pub(), Amount: 80})),
		g.tx(op, "", core.TxSessionOpen, 0, core.SessionOpenPayload{SessionID: "s2", GameID: "solo", Players: []string{op.pub()}}),
		g.failing(g.tx(carol, "", core.TxTransferAsset, 0, core.TransferAssetPayload{AssetID: swordID, To: bob.pub()})),
	)

	g.block(
		g.tx(op, "", core.TxRandomReveal, 0, core.RandomRevealPayload{CommitID: "r1", Secret: "s3cret"}),
		g.tx(op, "", core.TxSessionCancel, 0, core.SessionCancelPayload{SessionID: "s2"}),
		g.tx(carol, "", core.TxUnequipItem, 0, core.UnequipItemPayload{AssetID: swordID}),
		g.tx(carol, "", core.TxTransferAsset, 0, core.TransferAssetPayload{AssetID: rewardSwordID, To: bob.pub()}),
		g.tx(bob, "", core.TxRevokeDelegation, 0, core.RevokeDelegationPayload{Grantee: dave.pub()}),
		g.failing(g.tx(dave, bob.pub(), core.TxTransfer, 0, core.TransferPayload{To: carol.pub(), Amount: 1})),
		g.tx(bob, "", core.TxListMarket, 0, core.ListMarketPayload{AssetID: rewardSwordID, Price: 7}),
	)
	g.block(
		g.failing(g.tx(bob, "", core.TxBurnAsset, 0, core.BurnAssetPayload{AssetID: rewardSwordID})),
		g.tx(carol, "", core.TxBurnAsset, 0, core.BurnAssetPayload{AssetID: swordID}),
	)
	g.block()

	g.dumpDB()

	g.rpc("getBlockHeight", "")
	g.rpc("getBlock", `{"height":1}`)
	g.rpc("getBlock", `{"hash":"nope"}`)
	g.rpc("getBalance", fmt.Sprintf(`{"address":%q}`, bob.pub()))
	g.rpc("getBalance", `{}`)
	g.rpc("getAsset", fmt.Sprintf(`{"id":%q}`, swordID))
	g.rpc("getAsset", `{"id":"missing"}`)
	g.rpc("getSession", `{"id":"s1"}`)
	g.rpc("getListing", fmt.Sprintf(`{"id":%q}`, listingID))
	g.rpc("getTemplate", `{"id":"sword"}`)
	g.rpc("getInventory", fmt.Sprintf(`{"owner":%q}`, carol.pub()))
	g.rpc("getRandomCommitment", `{"id":"r1"}`)
	g.rpc("getAssetsByOwner", fmt.Sprintf(`{"owner":%q}`, bob.pub()))
	g.rpc("getAssetsByOwner", fmt.Sprintf(`{"owner":%q,"limit":1}`, bob.pub()))
	g.rpc("getSessionsByPlayer", fmt.Sprintf(`{"player":%q}`, carol.pub()))
	g.rpc("getSessionsByPlayer", fmt.Sprintf(`{"player":%q,"offset":1,"limit":5}`, carol.pub()))
	g.rpc("getActiveListings", "")
	g.rpc("getActiveListings", `{"limit":3}`)
	g.rpc("getTxStatus", fmt.Sprintf(`{"tx_id":%q}`, rewardTx.ID))
	g.rpc("getTxStatus", `{"tx_id":"unknown"}`)
	g.rpc("getDelegation", fmt.Sprintf(`{"granter":%q,"grantee":%q}`, bob.pub(), dave.pub()))
	g.rpc("getDelegationsByGranter", fmt.Sprintf(`{"granter":%q}`, bob.pub()))
	g.rpc("getDelegationsByGrantee", fmt.Sprintf(`{"grantee":%q}`, dave.pub()))
	g.rpc("getMempoolSize", "")
	g.rpc("sendTx", `{"chain_id":"other"}`)
	g.rpc("sendTx", fmt.Sprintf(`{"chain_id":%q,"type":"transfer","from":%q,"payload":{},"timestamp":1}`, goldenChainID, bob.pub()))
	g.rpc("getBalance", `not-json`)
	g.rpc("noSuchMethod", "")

	compareGolden(t, "golden_chain.txt", g.out.String())

	// A follower replaying the stored blocks through ExecuteBlock must reach
	// the same state roots the producer signed.
	follower := newGoldenChain(t, t.TempDir(), op, prop, map[string]uint64{op.pub(): 1_000_000, alice.pub(): 5_000})
	for _, b := range g.blocks[1:] {
		if err := follower.exec.ExecuteBlock(b); err != nil {
			t.Fatalf("replay block %d: %v", b.Header.Height, err)
		}
		root, err := follower.state.ComputeRoot()
		if err != nil {
			t.Fatal(err)
		}
		if root != b.Header.StateRoot {
			t.Fatalf("replay block %d: state root %s, want %s", b.Header.Height, root, b.Header.StateRoot)
		}
		if err := follower.bc.AddBlock(b); err != nil {
			t.Fatal(err)
		}
		if err := follower.state.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestGoldenFormats pins encodings that are not exercised by TestGoldenChain:
// signing input with and without on_behalf_of, consent bytes, addresses,
// config JSON field names and P2P envelopes.
func TestGoldenFormats(t *testing.T) {
	var out strings.Builder
	k := newGoldenKey(9)
	tx := &core.Transaction{ChainID: "c", Type: core.TxTransfer, From: k.pub(), Nonce: 3, Fee: 2,
		Timestamp: 42, Payload: json.RawMessage(`{"to":"t","amount":1}`)}
	tx.Sign(k.priv)
	txJSON, _ := json.Marshal(tx)
	fmt.Fprintf(&out, "tx id=%s sig=%s\ntx json=%s\n", tx.ID, tx.Signature, txJSON)
	tx.OnBehalfOf = newGoldenKey(8).pub()
	tx.Sign(k.priv)
	fmt.Fprintf(&out, "delegated tx id=%s\n", tx.ID)
	fmt.Fprintf(&out, "empty tx_root=%s\n", core.ComputeTxRoot(nil))
	fmt.Fprintf(&out, "consent=%s\n", core.SessionConsentMessage("tolchain-dev", "sess:1", "game", 100))
	fmt.Fprintf(&out, "address=%s\n", k.priv.Public().Address())
	fmt.Fprintf(&out, "state prefixes=%v\n", storage.StatePrefixes())

	cfg := config.DefaultConfig()
	cfg.Validators = []string{k.pub()}
	cfg.TLS = &config.TLSConfig{}
	cfg.SeedPeers = []config.SeedPeer{{ID: "n1", Addr: "h:1"}}
	cfgJSON, _ := json.Marshal(cfg)
	fmt.Fprintf(&out, "config=%s\n", cfgJSON)

	for _, v := range []any{
		network.Message{Type: network.MsgGetBlocks, Payload: json.RawMessage(`{"from_height":1,"limit":2}`)},
		network.GetBlocksRequest{FromHeight: 1, Limit: 2},
		network.BlocksResponse{Blocks: []*core.Block{}},
	} {
		data, _ := json.Marshal(v)
		fmt.Fprintf(&out, "p2p %T=%s\n", v, data)
	}
	compareGolden(t, "golden_formats.txt", out.String())
}
