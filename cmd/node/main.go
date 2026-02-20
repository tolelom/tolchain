// Command node starts a TOL Chain node.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tolelom/tolchain/config"
	"github.com/tolelom/tolchain/consensus"
	"github.com/tolelom/tolchain/core"
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
	password := flag.String("password", "", "keystore password")
	genKey := flag.Bool("genkey", false, "generate a new validator key and exit")
	flag.Parse()

	// ---- generate key mode ----
	if *genKey {
		w, err := wallet.Generate()
		if err != nil {
			log.Fatal(err)
		}
		if err := wallet.SaveKey(*keyPath, *password, w.PrivKey()); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Generated key. Public key (validator address): %s\n", w.PubKey())
		fmt.Printf("Saved to: %s\n", *keyPath)
		return
	}

	// ---- load config ----
	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// ---- load validator key ----
	privKey, err := wallet.LoadKey(*keyPath, *password)
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

	// ---- network ----
	p2pAddr := fmt.Sprintf(":%d", cfg.P2PPort)
	node := network.NewNode(cfg.NodeID, p2pAddr, mempool)
	_ = network.NewSyncer(node, bc, poa, exec, state)
	if err := node.Start(); err != nil {
		log.Fatalf("p2p start: %v", err)
	}
	defer node.Stop()
	log.Printf("P2P listening on %s", p2pAddr)

	// ---- RPC ----
	rpcAddr := fmt.Sprintf(":%d", cfg.RPCPort)
	rpcHandler := rpc.NewHandler(bc, mempool, state, idx, cfg.Genesis.ChainID)
	rpcServer := rpc.NewServer(rpcAddr, rpcHandler)
	if err := rpcServer.Start(); err != nil {
		log.Fatalf("rpc start: %v", err)
	}
	defer rpcServer.Stop()
	log.Printf("RPC listening on %s", rpcAddr)

	// ---- consensus loop ----
	done := make(chan struct{})
	go poa.Run(2*time.Second, done)
	log.Printf("Consensus running (validator: %s)", privKey.Public().Hex())

	// ---- graceful shutdown ----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	close(done)
	log.Println("Shutting down.")
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
