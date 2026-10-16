package config

import (
	"strings"
	"testing"
)

func TestValidate_RejectsNegativeTunables(t *testing.T) {
	cases := map[string]func(*Config){
		"max_block_txs":                func(c *Config) { c.MaxBlockTxs = -1 },
		"mempool.max_size":             func(c *Config) { c.Mempool.MaxSize = -1 },
		"mempool.max_tx_age_sec":       func(c *Config) { c.Mempool.MaxTxAgeSec = -1 },
		"mempool.max_future_sec":       func(c *Config) { c.Mempool.MaxFutureSec = -1 },
		"network.max_peers":            func(c *Config) { c.Network.MaxPeers = -1 },
		"network.write_timeout_sec":    func(c *Config) { c.Network.WriteTimeoutSec = -1 },
		"network.read_timeout_sec":     func(c *Config) { c.Network.ReadTimeoutSec = -1 },
		"network.max_message_size":     func(c *Config) { c.Network.MaxMessageSize = -1 },
		"network.sync_batch_size":      func(c *Config) { c.Network.SyncBatchSize = -1 },
		"network.max_sync_batch_size":  func(c *Config) { c.Network.MaxSyncBatchSize = -1 },
		"rpc.rate_limit":               func(c *Config) { c.RPC.RateLimit = -1 },
		"rpc.rate_burst":               func(c *Config) { c.RPC.RateBurst = -1 },
		"rpc.read_timeout_sec":         func(c *Config) { c.RPC.ReadTimeoutSec = -1 },
		"rpc.write_timeout_sec":        func(c *Config) { c.RPC.WriteTimeoutSec = -1 },
		"rpc.idle_timeout_sec":         func(c *Config) { c.RPC.IdleTimeoutSec = -1 },
		"rpc.max_body_size":            func(c *Config) { c.RPC.MaxBodySize = -1 },
		"rpc.sse_buffer_size":          func(c *Config) { c.RPC.SSEBufferSize = -1 },
		"consensus.block_interval_sec": func(c *Config) { c.Consensus.BlockIntervalSec = -1 },
		"consensus.max_time_drift_sec": func(c *Config) { c.Consensus.MaxTimeDriftSec = -1 },
	}
	for field, mutate := range cases {
		cfg := validConfig(t)
		mutate(cfg)
		cfg.ApplyDefaults()
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("%s = -1: want an error naming the field, got %v", field, err)
		}
	}
}

func TestValidate_RejectsMalformedOperator(t *testing.T) {
	cfg := validConfig(t)
	cfg.Operators = []string{validatorHex(t), "not-a-key"}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "operators[1]") {
		t.Fatalf("want operators[1] error, got %v", err)
	}
}

func TestValidate_RejectsEmptyOperators(t *testing.T) {
	cfg := validConfig(t)
	cfg.Operators = nil
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "operators") {
		t.Fatalf("a chain with no operators would reject every mint and session; want an operators error, got %v", err)
	}
}

func TestValidate_RejectsUppercaseKeys(t *testing.T) {
	for _, field := range []string{"validators", "operators"} {
		cfg := validConfig(t)
		upper := strings.ToUpper(validatorHex(t))
		if field == "validators" {
			cfg.Validators = []string{upper}
		} else {
			cfg.Operators = []string{upper}
		}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), field+"[0]") {
			t.Errorf("uppercase key never matches a lowercase tx sender; want %s[0] error, got %v", field, err)
		}
	}
}

func TestValidate_RejectsAllocToNonCanonicalKey(t *testing.T) {
	cfg := validConfig(t)
	cfg.Genesis.Alloc = map[string]uint64{strings.ToUpper(validatorHex(t)): 1}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "genesis.alloc") {
		t.Fatalf("an alloc key nobody can sign for locks its balance forever; want genesis.alloc error, got %v", err)
	}
}
