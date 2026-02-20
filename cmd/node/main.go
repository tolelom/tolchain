// Command node starts a TOL Chain node.
package main

import (
	"flag"
	"fmt"
	"log"
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
	_ "github.com/tolelom/tolchain/vm/modules/market"
	_ "github.com/tolelom/tolchain/vm/modules/session"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to config file")
	keyPath := flag.String("key", "validator.key", "path to keystore file")
	genKey := flag.Bool("genkey", false, "generate a new validator key and exit")
	genCerts := flag.String("gencerts", "", "generate CA + node TLS certs into the given directory and exit (requires node ID from config)")
	flag.Parse()

	// Read keystore password from environment (not CLI flags — they leak via ps).
	password := os.Getenv("TOL_PASSWORD")
	if password == "" {
		log.Println("WARNING: TOL_PASSWORD not set — keystore will use an empty password")
	}

	// ---- generate key mode ----
	if *genKey {
		w, err := wallet.Generate()
		if err != nil {
			log.Fatal(err)
		}
		if err := wallet.SaveKey(*keyPath, password, w.PrivKey()); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Generated key. Public key (validator address): %s\n", w.PubKey())
		fmt.Printf("Saved to: %s\n", *keyPath)
		return
	}

	// ---- generate certs mode ----
	if *genCerts != "" {
		cfgForCerts, err := loadConfig(*cfgPath)
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		if err := certgen.GenerateAll(*genCerts, cfgForCerts.NodeID, nil); err != nil {
			log.Fatalf("gencerts: %v", err)
		}
		fmt.Printf("Certificates generated in %s for node %q\n", *genCerts, cfgForCerts.NodeID)
		return
	}

	// ---- load config ----
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// ---- load validator key ----
	privKey, err := wallet.LoadKey(*keyPath, password)
	if err != nil {
		log.Fatalf("load key: %v", err)
	}

	// ---- open DB ----
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		log.Fatalf("mkdir data dir: %v", err)
	}
	db, err := storage.NewLevelDB(cfg.DataDir + "/chain")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	stateDB := db // reuse same DB with different key prefixes
	blockStore := storage.NewLevelBlockStore(db)

	// ---- initialise state ----
	state := storage.NewStateDB(stateDB)

	// ---- initialise blockchain ----
	bc := core.NewBlockchain(blockStore)
	if err := bc.Init(); err != nil {
		log.Fatalf("blockchain init: %v", err)
	}

	// ---- genesis block (if fresh chain) ----
	if bc.Tip() == nil {
		genesisBlock, err := config.CreateGenesisBlock(cfg, state, privKey)
		if err != nil {
			log.Fatalf("genesis: %v", err)
		}
		if err := bc.AddBlock(genesisBlock); err != nil {
			log.Fatalf("add genesis: %v", err)
		}
		log.Printf("Genesis block committed: %s", genesisBlock.Hash)
	}

	// ---- events ----
	emitter := events.NewEmitter()

	// ---- indexer ----
	idx := indexer.New(db, emitter)

	// ---- mempool ----
	mempool := core.NewMempool()

	// ---- VM executor ----
	exec := vm.NewExecutor(state, emitter)

	// ---- consensus ----
	poa := consensus.New(cfg, bc, state, mempool, exec, emitter, privKey)

	// ---- TLS ----
	tlsCfg, err := config.LoadTLSConfig(cfg.TLS)
	if err != nil {
		log.Fatalf("tls: %v", err)
	}
	if tlsCfg != nil {
		log.Println("mTLS enabled for P2P")
	}

	// ---- network ----
	p2pAddr := fmt.Sprintf(":%d", cfg.P2PPort)
	node := network.NewNode(cfg.NodeID, p2pAddr, mempool, tlsCfg)
	syncer := network.NewSyncer(node, bc, poa, exec, state)
	if err := node.Start(); err != nil {
		log.Fatalf("p2p start: %v", err)
	}
	defer node.Stop()
	log.Printf("P2P listening on %s", p2pAddr)

	// ---- connect to seed peers ----
	for _, sp := range cfg.SeedPeers {
		if err := node.AddPeer(sp.ID, sp.Addr); err != nil {
			log.Printf("seed peer %s (%s): %v", sp.ID, sp.Addr, err)
			continue
		}
		// Trigger initial block sync with the newly connected peer.
		if peer := node.Peer(sp.ID); peer != nil {
			syncer.SyncWithPeer(peer)
		}
		log.Printf("Connected to seed peer %s (%s)", sp.ID, sp.Addr)
	}

	// ---- RPC ----
	rpcAddr := fmt.Sprintf(":%d", cfg.RPCPort)
	rpcHandler := rpc.NewHandler(bc, mempool, state, idx, cfg.Genesis.ChainID)
	rpcServer := rpc.NewServer(rpcAddr, rpcHandler, cfg.RPCAuthToken)
	if err := rpcServer.Start(); err != nil {
		log.Fatalf("rpc start: %v", err)
	}
	defer rpcServer.Stop()
	log.Printf("RPC listening on %s", rpcAddr)
	if cfg.RPCAuthToken != "" {
		log.Println("RPC Bearer token authentication enabled")
	}

	// ---- consensus loop ----
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		poa.Run(2*time.Second, done)
	}()
	log.Printf("Consensus running (validator: %s)", privKey.Public().Hex())

	// ---- graceful shutdown ----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("Shutting down...")

	// 1. Stop consensus first (no new blocks written)
	close(done)
	wg.Wait()

	// 2. Deferred calls run in LIFO: rpcServer.Stop → node.Stop → db.Close
	log.Println("Shutdown complete.")
}

func loadConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("Config file not found at %s, using defaults.", path)
			return config.DefaultConfig(), nil
		}
		return nil, err
	}
	return cfg, nil
}
