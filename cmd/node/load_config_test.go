package main

import (
	"path/filepath"
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
