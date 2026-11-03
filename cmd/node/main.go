// Command node starts a TOL Chain node.
package main

import (
	"errors"
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
)

const (
	nodeVersion          = "0.1.0"
	seedRetryMinInterval = 5 * time.Second
	seedRetryMaxInterval = 2 * time.Minute
)

type flags struct {
	cfgPath     string
	cfgExplicit bool
	keyPath     string
	genKey      bool
	force       bool
	genCerts    string
}

func parseFlags() flags {
	var f flags
	flag.StringVar(&f.cfgPath, "config", "config.json", "path to config file")
	flag.StringVar(&f.keyPath, "key", "validator.key", "path to keystore file")
	flag.BoolVar(&f.genKey, "genkey", false, "generate a new validator key and exit")
	flag.BoolVar(&f.force, "force", false, "with -genkey: overwrite an existing keystore file")
	flag.StringVar(&f.genCerts, "gencerts", "", "generate CA + node TLS certs into the given directory and exit (requires node ID from config)")
	flag.Parse()
	flag.Visit(func(fl *flag.Flag) { f.cfgExplicit = f.cfgExplicit || fl.Name == "config" })
	return f
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	f := parseFlags()

	// From the environment, not a flag: flags are visible in ps.
	password := os.Getenv("TOL_PASSWORD")
	if password == "" {
		slog.Warn("TOL_PASSWORD not set — keystore will use an empty password")
	}

	var err error
	switch {
	case f.genKey:
		err = generateKey(f, password)
	case f.genCerts != "":
		err = generateCerts(f)
	default:
		err = runNode(f, password)
	}
	if err != nil {
		slog.Error("node failed", "error", err)
		os.Exit(1)
	}
}

// generateKey refuses to overwrite an existing keystore without -force:
// losing it locks the validator out of its identity for good.
func generateKey(f flags, password string) error {
	if !f.force {
		if _, err := os.Stat(f.keyPath); err == nil {
			return fmt.Errorf("keystore %s already exists; refusing to overwrite (use -force to override)", f.keyPath)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat keystore: %w", err)
		}
	}
	w, err := wallet.Generate()
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	if err := wallet.SaveKey(f.keyPath, password, w.PrivKey()); err != nil {
		return fmt.Errorf("save key: %w", err)
	}
	slog.Info("key generated", "pubkey", w.PubKey(), "path", f.keyPath)
	return nil
}

func generateCerts(f flags) error {
	cfg, err := loadConfig(f.cfgPath, f.cfgExplicit)
	if err != nil {
		return fmt.Errorf("config load: %w", err)
	}
	if err := certgen.GenerateAll(f.genCerts, cfg.NodeID, nil); err != nil {
		return fmt.Errorf("gencerts: %w", err)
	}
	slog.Info("certificates generated", "dir", f.genCerts, "node_id", cfg.NodeID)
	return nil
}

func runNode(f flags, password string) error {
	startTime := time.Now()
	cfg, err := loadConfig(f.cfgPath, f.cfgExplicit)
	if err != nil {
		return fmt.Errorf("config load: %w", err)
	}
	setLogLevel(cfg.LogLevel)

	privKey, err := wallet.LoadKey(f.keyPath, password)
	if err != nil {
		return fmt.Errorf("load key: %w", err)
	}
	ensureDevValidator(cfg, privKey.Public().Hex())

	db, state, bc, err := openChain(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	emitter := events.NewEmitter()
	idx := indexer.New(db, emitter)
	mempool := core.NewMempool(cfg.Mempool.MaxSize, cfg.Mempool.MaxTxAgeSec, cfg.Mempool.MaxFutureSec)
	exec := newExecutor(cfg, state, bc, emitter)

	poa := consensus.New(cfg, bc, state, mempool, exec, emitter, privKey)
	if recovered, err := poa.RecoverTipState(); err != nil {
		return fmt.Errorf("state does not match the stored tip: %w", err)
	} else if recovered {
		slog.Warn("replayed the tip block: its state commit was missing", "height", bc.Height())
	}

	node, syncer, err := startP2P(cfg, bc, poa, exec, state, emitter, mempool)
	if err != nil {
		return err
	}
	defer node.Stop()
	broadcastAndPruneCommittedBlocks(cfg, db, emitter, node)

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		keepSeedsConnected(cfg.SeedPeers, node, syncer, done)
	}()

	status := func() any { return nodeStatus(cfg, bc, node, mempool, startTime) }
	rpcServer, err := startRPC(cfg, bc, mempool, state, idx, emitter, status)
	if err != nil {
		return err
	}
	defer func() { _ = rpcServer.Stop() }()

	wg.Add(1)
	go func() {
		defer wg.Done()
		poa.Run(time.Duration(cfg.Consensus.BlockIntervalSec)*time.Second, done)
	}()
	slog.Info("consensus running", "validator", privKey.Public().Hex())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	slog.Info("shutting down...")

	// Stop block production before the deferred RPC, P2P and DB shutdown.
	close(done)
	wg.Wait()
	slog.Info("shutdown complete")
	return nil
}

func setLogLevel(name string) {
	level := slog.LevelInfo
	switch name {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))
}

// openChain opens the shared LevelDB (state, blocks and indexes live under
// different prefixes) and writes genesis on an empty database. An existing
// database keeps the genesis it was created with.
func openChain(cfg *config.Config) (*storage.LevelDB, *storage.StateDB, *core.Blockchain, error) {
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, nil, nil, fmt.Errorf("mkdir data dir: %w", err)
	}
	db, err := storage.NewLevelDB(cfg.DataDir + "/chain")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open db: %w", err)
	}
	state := storage.NewStateDB(db)
	bc := core.NewBlockchain(storage.NewLevelBlockStore(db))
	err = bc.Init()
	if err == nil && bc.Tip() == nil {
		err = commitGenesis(cfg, state, bc)
	}
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, err
	}
	return db, state, bc, nil
}

func commitGenesis(cfg *config.Config, state core.State, bc *core.Blockchain) error {
	genesis, err := config.CreateGenesisBlock(cfg, state)
	if err != nil {
		return fmt.Errorf("genesis creation: %w", err)
	}
	if err := bc.AddBlock(genesis); err != nil {
		return fmt.Errorf("add genesis: %w", err)
	}
	slog.Info("genesis block committed", "hash", genesis.Hash)
	return nil
}

func newExecutor(cfg *config.Config, state core.State, bc *core.Blockchain, emitter *events.Emitter) *vm.Executor {
	exec := vm.NewExecutor(state, emitter)
	exec.SetOperators(cfg.Operators)
	exec.SetMaxTotalSupply(cfg.MaxTotalSupply)
	exec.SetBlockHashes(func(height int64) (string, error) {
		b, err := bc.GetBlockByHeight(height)
		if err != nil {
			return "", err
		}
		return b.Hash, nil
	})
	return exec
}

func startP2P(cfg *config.Config, bc *core.Blockchain, poa *consensus.PoA, exec *vm.Executor, state core.State,
	emitter *events.Emitter, mempool *core.Mempool) (*network.Node, *network.Syncer, error) {
	tlsCfg, err := config.LoadTLSConfig(cfg.TLS)
	if err != nil {
		return nil, nil, fmt.Errorf("tls config: %w", err)
	}
	if tlsCfg != nil {
		slog.Info("mTLS enabled for P2P")
	}
	addr := fmt.Sprintf(":%d", cfg.P2PPort)
	n := cfg.Network
	node := network.NewNode(cfg.NodeID, addr, cfg.Genesis.ChainID, mempool, tlsCfg, n.MaxPeers, n.WriteTimeoutSec, n.ReadTimeoutSec, n.MaxMessageSize)
	syncer := network.NewSyncer(node, bc, poa, exec, state, emitter, mempool, n.SyncBatchSize, n.MaxSyncBatchSize)
	if err := node.Start(); err != nil {
		return nil, nil, fmt.Errorf("p2p start: %w", err)
	}
	slog.Info("P2P listening", "addr", addr)
	return node, syncer, nil
}

// broadcastAndPruneCommittedBlocks relays blocks this node produced and prunes
// old ones after each.
func broadcastAndPruneCommittedBlocks(cfg *config.Config, db storage.DB, emitter *events.Emitter, node *network.Node) {
	var pruner *storage.Pruner
	if cfg.PruneKeepBlocks > 0 {
		pruner = storage.NewPruner(db, cfg.PruneKeepBlocks)
		slog.Info("block pruning enabled", "keep_blocks", cfg.PruneKeepBlocks)
	}
	emitter.Subscribe(events.EventBlockCommit, func(ev events.Event) {
		b, ok := ev.Data["block"].(*core.Block)
		if !ok || b == nil {
			return
		}
		node.BroadcastBlock(b)
		if pruner != nil {
			if err := pruner.Prune(b.Header.Height); err != nil {
				slog.Error("block pruning failed", "error", err)
			}
		}
	})
}

// keepSeedsConnected connects to every seed now, then retries missing ones
// with exponential backoff until done is closed.
func keepSeedsConnected(seeds []config.SeedPeer, node *network.Node, syncer *network.Syncer, done <-chan struct{}) {
	if connected := connectSeeds(seeds, node, syncer); len(seeds) > 0 && connected == 0 {
		slog.Warn("failed to connect to any seed peer — node is isolated")
	}
	interval := seedRetryMinInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if connectSeeds(seeds, node, syncer) == len(seeds) {
				interval = seedRetryMinInterval
			} else {
				interval = min(interval*2, seedRetryMaxInterval)
			}
			ticker.Reset(interval)
		}
	}
}

// connectSeeds dials the seeds that are not connected and returns how many
// are connected afterwards.
func connectSeeds(seeds []config.SeedPeer, node *network.Node, syncer *network.Syncer) int {
	connected := 0
	for _, sp := range seeds {
		if node.Peer(sp.ID) != nil {
			connected++
			continue
		}
		if err := node.AddPeer(sp.ID, sp.Addr); err != nil {
			slog.Warn("seed peer connection failed", "peer_id", sp.ID, "addr", sp.Addr, "error", err)
			continue
		}
		if peer := node.Peer(sp.ID); peer != nil {
			syncer.SyncWithPeer(peer)
		}
		connected++
		slog.Info("connected to seed peer", "peer_id", sp.ID, "addr", sp.Addr)
	}
	return connected
}

func startRPC(cfg *config.Config, bc *core.Blockchain, mempool *core.Mempool, state core.State, idx *indexer.Indexer,
	emitter *events.Emitter, status func() any) (*rpc.Server, error) {
	addr := fmt.Sprintf(":%d", cfg.RPCPort)
	handler := rpc.NewHandler(bc, mempool, state, idx, cfg.Genesis.ChainID)
	sse := rpc.NewSSEBroker(emitter, cfg.RPCAuthToken, cfg.RPC.SSEBufferSize)
	r := cfg.RPC
	server := rpc.NewServer(addr, handler, cfg.RPCAuthToken, sse, status, r.RateLimit, r.RateBurst,
		r.ReadTimeoutSec, r.WriteTimeoutSec, r.IdleTimeoutSec, r.MaxBodySize, r.TrustedProxies)
	if err := server.Start(); err != nil {
		return nil, fmt.Errorf("rpc start: %w", err)
	}
	slog.Info("RPC listening", "addr", addr)
	if cfg.RPCAuthToken != "" {
		slog.Info("RPC Bearer token authentication enabled")
	}
	return server, nil
}

func nodeStatus(cfg *config.Config, bc *core.Blockchain, node *network.Node, mempool *core.Mempool, startTime time.Time) map[string]any {
	var height int64
	var hash string
	if tip := bc.Tip(); tip != nil {
		height, hash = tip.Header.Height, tip.Hash
	}
	return map[string]any{
		"node_id":        cfg.NodeID,
		"chain_id":       cfg.Genesis.ChainID,
		"block_height":   height,
		"block_hash":     hash,
		"peer_count":     node.PeerCount(),
		"mempool_size":   mempool.Size(),
		"uptime_seconds": int(time.Since(startTime).Seconds()),
		"version":        nodeVersion,
	}
}

// loadConfig falls back to the dev defaults only when -config was not given
// and the default file is missing.
func loadConfig(path string, explicit bool) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			slog.Info("config file not found, using defaults (dev mode)", "path", path)
			return config.DefaultConfig(), nil
		}
		return nil, err
	}
	return cfg, nil
}

// ensureDevValidator makes the loaded key the sole validator when the config
// lists none (dev defaults), so validation passes.
func ensureDevValidator(cfg *config.Config, pubKeyHex string) {
	if len(cfg.Validators) == 0 {
		cfg.Validators = []string{pubKeyHex}
		slog.Info("dev mode: auto-registered validator", "pubkey", pubKeyHex)
	}
}
