package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_ExplicitPathMustExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "config.json")
	if _, err := loadConfig(missing, true); err == nil {
		t.Fatal("an explicitly given config path that does not exist must fail, not start a dev chain")
	}
}

func TestLoadConfig_DefaultPathFallsBackToDevMode(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "config.json"), false)
	if err != nil || cfg == nil {
		t.Fatalf("missing default config should fall back to dev mode, got %v", err)
	}
}

func TestEnsureDevValidator_RegistersKeyAsOperator(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "config.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	pub := strings.Repeat("ab", 32)
	ensureDevValidator(cfg, pub)
	if len(cfg.Operators) != 1 || cfg.Operators[0] != pub {
		t.Fatalf("dev chain must let its own key mint and open sessions, operators = %v", cfg.Operators)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("dev config should validate after auto-registration: %v", err)
	}
}
