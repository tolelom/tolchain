package config

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tolelom/tolchain/crypto"
	"github.com/tolelom/tolchain/internal/testutil"
)

// validConfig returns a Config that passes Validate.
func validConfig(t *testing.T) *Config {
	t.Helper()
	_, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Validators = []string{pub.Hex()}
	return cfg
}

// validatorHex generates a deterministic 32-byte ed25519 public key hex for testing.
func validatorHex(t *testing.T) string {
	t.Helper()
	_, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return pub.Hex()
}

func writeConfigFile(t *testing.T, cfg *Config) string {
	t.Helper()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---- DefaultConfig ----

func TestDefaultConfig_HasDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.NodeID == "" {
		t.Error("NodeID is empty")
	}
	if cfg.DataDir == "" {
		t.Error("DataDir is empty")
	}
	if cfg.RPCPort != DefaultRPCPort {
		t.Errorf("RPCPort = %d, want %d", cfg.RPCPort, DefaultRPCPort)
	}
	if cfg.P2PPort != DefaultP2PPort {
		t.Errorf("P2PPort = %d, want %d", cfg.P2PPort, DefaultP2PPort)
	}
	if cfg.Genesis.ChainID == "" {
		t.Error("Genesis.ChainID is empty")
	}
}

func TestDefaultConfig_ValidateFails_NoValidators(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err == nil {
		t.Error("expected Validate to fail for default config (no validators)")
	}
}

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.RPCPort != DefaultRPCPort {
		t.Errorf("RPCPort: got %d want %d", cfg.RPCPort, DefaultRPCPort)
	}
	if cfg.P2PPort != DefaultP2PPort {
		t.Errorf("P2PPort: got %d want %d", cfg.P2PPort, DefaultP2PPort)
	}
	if cfg.MaxBlockTxs != DefaultMaxBlockTxs {
		t.Errorf("MaxBlockTxs: got %d want %d", cfg.MaxBlockTxs, DefaultMaxBlockTxs)
	}
	if cfg.NodeID != "node0" {
		t.Errorf("NodeID: got %q want %q", cfg.NodeID, "node0")
	}
}

// ---- Load ----

func TestLoad_ValidConfig(t *testing.T) {
	v := validatorHex(t)
	cfg := &Config{
		NodeID:      "node0",
		DataDir:     "./data",
		RPCPort:     8545,
		P2PPort:     30303,
		MaxBlockTxs: 100,
		Validators:  []string{v},
		Genesis: GenesisConfig{
			ChainID: "test-chain",
			Alloc:   map[string]uint64{v: 1000},
		},
	}

	path := writeConfigFile(t, cfg)
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.NodeID != "node0" {
		t.Errorf("NodeID: got %q want %q", loaded.NodeID, "node0")
	}
	if loaded.Genesis.ChainID != "test-chain" {
		t.Errorf("ChainID: got %q want %q", loaded.Genesis.ChainID, "test-chain")
	}
	if len(loaded.Validators) != 1 || loaded.Validators[0] != v {
		t.Errorf("Validators mismatch")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	v := validatorHex(t)
	// Write a minimal config that omits RPCPort, P2PPort, MaxBlockTxs.
	raw := map[string]any{
		"node_id":    "minimal",
		"data_dir":   "./data",
		"validators": []string{v},
		"genesis":    map[string]any{"chain_id": "test"},
	}
	data, _ := json.Marshal(raw)
	path := filepath.Join(t.TempDir(), "min.json")
	_ = os.WriteFile(path, data, 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Defaults should be applied via DefaultConfig() base.
	if cfg.RPCPort != DefaultRPCPort {
		t.Errorf("RPCPort should default to %d, got %d", DefaultRPCPort, cfg.RPCPort)
	}
	if cfg.P2PPort != DefaultP2PPort {
		t.Errorf("P2PPort should default to %d, got %d", DefaultP2PPort, cfg.P2PPort)
	}
	if cfg.MaxBlockTxs != DefaultMaxBlockTxs {
		t.Errorf("MaxBlockTxs should default to %d, got %d", DefaultMaxBlockTxs, cfg.MaxBlockTxs)
	}
}

func TestLoad_NonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json!!!"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoad_ValidatesAfterParsing(t *testing.T) {
	// Write a JSON file that parses but has no validators
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")
	cfg := DefaultConfig() // no validators
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = Load(path)
	if err == nil {
		t.Error("expected Load to fail validation for config with no validators")
	}
}

// ---- Validate success ----

func TestValidate_ValidConfig(t *testing.T) {
	cfg := validConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// ---- Validate failures ----

func TestValidate_EmptyNodeID(t *testing.T) {
	cfg := validConfig(t)
	cfg.NodeID = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty NodeID")
	}
}

func TestValidate_EmptyDataDir(t *testing.T) {
	cfg := validConfig(t)
	cfg.DataDir = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty DataDir")
	}
}

func TestValidate_EmptyChainID(t *testing.T) {
	cfg := validConfig(t)
	cfg.Genesis.ChainID = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty ChainID")
	}
}

func TestValidate_InvalidRPCPort_Zero(t *testing.T) {
	cfg := validConfig(t)
	cfg.RPCPort = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for RPCPort=0")
	}
}

func TestValidate_InvalidRPCPort_TooHigh(t *testing.T) {
	cfg := validConfig(t)
	cfg.RPCPort = 65536
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for RPCPort=65536")
	}
}

func TestValidate_InvalidP2PPort_Zero(t *testing.T) {
	cfg := validConfig(t)
	cfg.P2PPort = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for P2PPort=0")
	}
}

func TestValidate_InvalidP2PPort_TooHigh(t *testing.T) {
	cfg := validConfig(t)
	cfg.P2PPort = 65536
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for P2PPort=65536")
	}
}

func TestValidate_SameRPCAndP2PPort(t *testing.T) {
	cfg := validConfig(t)
	cfg.RPCPort = 9000
	cfg.P2PPort = 9000
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when RPCPort == P2PPort")
	}
}

func TestValidate_EmptyValidators(t *testing.T) {
	cfg := validConfig(t)
	cfg.Validators = []string{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty validators")
	}
}

func TestValidate_InvalidValidatorHex(t *testing.T) {
	cfg := validConfig(t)
	cfg.Validators = []string{"not-valid-hex"}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid validator hex")
	}
}

func TestValidate_ValidatorWrongLength(t *testing.T) {
	cfg := validConfig(t)
	// 16 bytes (32 hex chars) instead of 32 bytes (64 hex chars)
	short := hex.EncodeToString(make([]byte, 16))
	cfg.Validators = []string{short}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for wrong length validator")
	}
}

func TestValidate_DuplicateValidator(t *testing.T) {
	v := validatorHex(t)
	cfg := DefaultConfig()
	cfg.Validators = []string{v, v}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for duplicate validator pubkey")
	}
}

// --- TLS ---

func TestValidate_PartialTLS(t *testing.T) {
	cfg := validConfig(t)
	cfg.TLS = &TLSConfig{
		CACert: "/path/to/ca.pem",
		// NodeCert and NodeKey left empty
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for partial TLS config")
	}
}

func TestValidate_FullTLS(t *testing.T) {
	cfg := validConfig(t)
	cfg.TLS = &TLSConfig{
		CACert:   "/path/to/ca.pem",
		NodeCert: "/path/to/node.pem",
		NodeKey:  "/path/to/node-key.pem",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate failed with full TLS: %v", err)
	}
}

func TestValidate_EmptyTLS(t *testing.T) {
	cfg := validConfig(t)
	cfg.TLS = &TLSConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate failed with all-empty TLS: %v", err)
	}
}

// ---- Save / Load ----

func TestSaveAndLoad_Roundtrip(t *testing.T) {
	cfg := validConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.NodeID != cfg.NodeID {
		t.Errorf("NodeID = %s, want %s", loaded.NodeID, cfg.NodeID)
	}
	if loaded.RPCPort != cfg.RPCPort {
		t.Errorf("RPCPort = %d, want %d", loaded.RPCPort, cfg.RPCPort)
	}
	if loaded.Genesis.ChainID != cfg.Genesis.ChainID {
		t.Errorf("ChainID = %s, want %s", loaded.Genesis.ChainID, cfg.Genesis.ChainID)
	}
	if len(loaded.Validators) != len(cfg.Validators) {
		t.Errorf("Validators count = %d, want %d", len(loaded.Validators), len(cfg.Validators))
	}
}

// ---- Genesis ----

func TestCreateGenesisBlock(t *testing.T) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Validators = []string{pub.Hex()}
	cfg.Genesis.Alloc = map[string]uint64{
		pub.Hex(): 1_000_000,
	}

	state := testutil.NewStateDB()

	block, err := CreateGenesisBlock(cfg, state, priv)
	if err != nil {
		t.Fatalf("CreateGenesisBlock: %v", err)
	}

	// Height must be 0
	if block.Header.Height != 0 {
		t.Errorf("Height = %d, want 0", block.Header.Height)
	}

	// Block must be signed
	if block.Hash == "" {
		t.Error("genesis block Hash is empty")
	}
	if block.Signature == "" {
		t.Error("genesis block Signature is empty")
	}

	// PrevHash must be the canonical genesis hash
	if block.Header.PrevHash != GenesisHash {
		t.Errorf("PrevHash = %s, want %s", block.Header.PrevHash, GenesisHash)
	}

	// Alloc balance should be set in state
	acc, err := state.GetAccount(pub.Hex())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if acc.Balance != 1_000_000 {
		t.Errorf("alloc balance = %d, want 1000000", acc.Balance)
	}
}

func TestCreateGenesisBlock_MultipleAlloc(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	_, pub2, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Validators = []string{priv.Public().Hex()}
	cfg.Genesis.Alloc = map[string]uint64{
		priv.Public().Hex(): 500,
		pub2.Hex():          300,
	}

	state := testutil.NewStateDB()
	block, err := CreateGenesisBlock(cfg, state, priv)
	if err != nil {
		t.Fatalf("CreateGenesisBlock: %v", err)
	}
	if block.Header.Height != 0 {
		t.Errorf("Height = %d, want 0", block.Header.Height)
	}

	acc1, err := state.GetAccount(priv.Public().Hex())
	if err != nil {
		t.Fatalf("GetAccount(proposer): %v", err)
	}
	if acc1.Balance != 500 {
		t.Errorf("proposer balance = %d, want 500", acc1.Balance)
	}

	acc2, err := state.GetAccount(pub2.Hex())
	if err != nil {
		t.Fatalf("GetAccount(pub2): %v", err)
	}
	if acc2.Balance != 300 {
		t.Errorf("pub2 balance = %d, want 300", acc2.Balance)
	}
}

func TestIsGenesisHash(t *testing.T) {
	if !IsGenesisHash(GenesisHash) {
		t.Error("IsGenesisHash(GenesisHash) = false, want true")
	}
	if IsGenesisHash("abcdef1234567890") {
		t.Error("IsGenesisHash(random) = true, want false")
	}
	if IsGenesisHash("") {
		t.Error("IsGenesisHash(\"\") = true, want false")
	}
}
