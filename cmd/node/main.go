// Command node starts a TOL Chain node.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/consensus"
	"github.com/tolelom/tolchain/core"
	"github.com/tolelom/tolchain/crypto/certgen"
	"github.com/tolelom/tolchain/events"
	"github.com/tolelom/tolchain/indexer"
	"github.com/tolelom/tolchain/network"
	"github.com/tolelom/tolchain/rpc"
	"github.com/tolelom/tolchain/storage"
	"github.com/tolelom/tolchain/vm"
	"github.com/tolelom/tolchain/wallet"

	// Import VM modules to trigger their init() self-registration.
	_ "github.com/tolelom/tolchain/vm/modules/asset"
	_ "github.com/tolelom/tolchain/vm/modules/economy"
	_ "github.com/tolelom/tolchain/vm/modules/inventory"
	_ "github.com/tolelom/tolchain/vm/modules/market"
	_ "github.com/tolelom/tolchain/vm/modules/random"
	_ "github.com/tolelom/tolchain/vm/modules/reward"
	_ "github.com/tolelom/tolchain/vm/modules/session"
)

func main() {
	// Initialize structured JSON logging.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	startTime := time.Now()

	cfgPath := flag.String("config", "config.json", "path to config file")
	keyPath := flag.String("key", "validator.key", "path to keystore file")
	genKey := flag.Bool("genkey", false, "generate a new validator key and exit")
	genCerts := flag.String("gencerts", "", "generate CA + node TLS certs into the given directory and exit (requires node ID from config)")
	flag.Parse()

	// Read keystore password from environment (not CLI flags — they leak via ps).
	password := os.Getenv("TOL_PASSWORD")
	if password == "" {
		slog.Warn("TOL_PASSWORD not set — keystore will use an empty password")
	}

	// ---- generate key mode ----
	if *genKey {
		w, err := wallet.Generate()
		if err != nil {
			slog.Error("generate key failed", "error", err)
			os.Exit(1)
		}
		if err := wallet.SaveKey(*keyPath, password, w.PrivKey()); err != nil {
			slog.Error("save key failed", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Generated key. Public key (validator address): %s\n", w.PubKey())
		fmt.Printf("Saved to: %s\n", *keyPath)
		return
	}

	// ---- generate certs mode ----
	if *genCerts != "" {
		cfgForCerts, err := loadConfig(*cfgPath)
		if err != nil {
			slog.Error("config load failed", "error", err)
			os.Exit(1)
		}
		if err := certgen.GenerateAll(*genCerts, cfgForCerts.NodeID, nil); err != nil {
			slog.Error("gencerts failed", "error", err)
			os.Exit(1)
		}
		fmt.Printf("Certificates generated in %s for node %q\n", *genCerts, cfgForCerts.NodeID)
		return
	}

	// ---- load config ----
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	// ---- load validator key ----
	privKey, err := wallet.LoadKey(*keyPath, password)
	if err != nil {
		slog.Error("load key failed", "error", err)
		os.Exit(1)
	}

	// If using defaults (no config file), auto-register the loaded key.
	ensureDevValidator(cfg, privKey.Public().Hex())

	// ---- open DB ----
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		slog.Error("mkdir data dir failed", "error", err)
		os.Exit(1)
	}
	db, err := storage.NewLevelDB(cfg.DataDir + "/chain")
	if err != nil {
		slog.Error("open db failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	stateDB := db // reuse same DB with different key prefixes
	blockStore := storage.NewLevelBlockStore(db)

	// ---- initialise state ----
	state := storage.NewStateDB(stateDB)

	// ---- initialise blockchain ----
	bc := core.NewBlockchain(blockStore)
	if err := bc.Init(); err != nil {
		slog.Error("blockchain init failed", "error", err)
		os.Exit(1)
	}

	// ---- genesis block (if fresh chain) ----
	if bc.Tip() == nil {
		genesisBlock, err := config.CreateGenesisBlock(cfg, state, privKey)
		if err != nil {
			slog.Error("genesis creation failed", "error", err)
			os.Exit(1)
		}
		if err := bc.AddBlock(genesisBlock); err != nil {
			slog.Error("add genesis failed", "error", err)
			os.Exit(1)
		}
		slog.Info("genesis block committed", "hash", genesisBlock.Hash)
	}

	// ---- events ----
	emitter := events.NewEmitter()

	// ---- indexer ----
	idx := indexer.New(db, emitter)

	// ---- mempool ----
	mempool := core.NewMempool()

	// ---- VM executor ----
	exec := vm.NewExecutor(state, emitter)
	exec.SetOperators(cfg.Operators)

	// ---- consensus ----
	poa := consensus.New(cfg, bc, state, mempool, exec, emitter, privKey)

	// ---- TLS ----
	tlsCfg, err := config.LoadTLSConfig(cfg.TLS)
	if err != nil {
		slog.Error("tls config failed", "error", err)
		os.Exit(1)
	}
	if tlsCfg != nil {
		slog.Info("mTLS enabled for P2P")
	}

	// ---- network ----
	p2pAddr := fmt.Sprintf(":%d", cfg.P2PPort)
	node := network.NewNode(cfg.NodeID, p2pAddr, cfg.Genesis.ChainID, mempool, tlsCfg)
	syncer := network.NewSyncer(node, bc, poa, exec, state, emitter, mempool)
	if err := node.Start(); err != nil {
		slog.Error("p2p start failed", "error", err)
		os.Exit(1)
	}
	defer node.Stop()
	slog.Info("P2P listening", "addr", p2pAddr)

	// ---- broadcast committed blocks ----
	emitter.Subscribe(events.EventBlockCommit, func(ev events.Event) {
		if b, ok := ev.Data["block"].(*core.Block); ok && b != nil {
			node.BroadcastBlock(b)
		}
	})

	// ---- connect to seed peers ----
	connectedSeeds := 0
	for _, sp := range cfg.SeedPeers {
		if err := node.AddPeer(sp.ID, sp.Addr); err != nil {
			slog.Warn("seed peer connection failed", "peer_id", sp.ID, "addr", sp.Addr, "error", err)
			continue
		}
		// Trigger initial block sync with the newly connected peer.
		if peer := node.Peer(sp.ID); peer != nil {
			syncer.SyncWithPeer(peer)
		}
		connectedSeeds++
		slog.Info("connected to seed peer", "peer_id", sp.ID, "addr", sp.Addr)
	}
	if len(cfg.SeedPeers) > 0 && connectedSeeds == 0 {
		slog.Warn("failed to connect to any seed peer — node is isolated")
	}

	// ---- RPC ----
	rpcAddr := fmt.Sprintf(":%d", cfg.RPCPort)
	rpcHandler := rpc.NewHandler(bc, mempool, state, idx, cfg.Genesis.ChainID)
	sseBroker := rpc.NewSSEBroker(emitter, cfg.RPCAuthToken)
	statusFunc := func() any {
		tip := bc.Tip()
		var blockHeight int64
		var blockHash string
		if tip != nil {
			blockHeight = tip.Header.Height
			blockHash = tip.Hash
		}
		return map[string]any{
			"node_id":        cfg.NodeID,
			"chain_id":       cfg.Genesis.ChainID,
			"block_height":   blockHeight,
			"block_hash":     blockHash,
			"peer_count":     node.PeerCount(),
			"mempool_size":   mempool.Size(),
			"uptime_seconds": int(time.Since(startTime).Seconds()),
			"version":        "0.1.0",
		}
	}
	rpcServer := rpc.NewServer(rpcAddr, rpcHandler, cfg.RPCAuthToken, sseBroker, statusFunc)
	if err := rpcServer.Start(); err != nil {
		slog.Error("rpc start failed", "error", err)
		os.Exit(1)
	}
	defer rpcServer.Stop()
	slog.Info("RPC listening", "addr", rpcAddr)
	if cfg.RPCAuthToken != "" {
		slog.Info("RPC Bearer token authentication enabled")
	}

	// ---- consensus loop ----
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		poa.Run(2*time.Second, done)
	}()
	slog.Info("consensus running", "validator", privKey.Public().Hex())

	// ---- graceful shutdown ----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down...")

	// 1. Stop consensus first (no new blocks written)
	close(done)
	wg.Wait()

	// 2. Deferred calls run in LIFO: rpcServer.Stop → node.Stop → db.Close
	slog.Info("shutdown complete")
}

func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("config file not found, using defaults (dev mode)", "path", path)
			return config.DefaultConfig(), nil
		}
		return nil, err
	}
	return cfg, nil
}

// ensureDevValidator adds the loaded key as the sole validator when using the
// default config (no config file). This prevents validation errors in dev mode.
func ensureDevValidator(cfg *config.Config, pubKeyHex string) {
	if len(cfg.Validators) == 0 {
		cfg.Validators = []string{pubKeyHex}
		slog.Info("dev mode: auto-registered validator", "pubkey", pubKeyHex)
	}
}
