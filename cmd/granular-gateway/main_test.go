package main

import (
	"path/filepath"
	"testing"

	gwconfig "github.com/clems4ever/granular/gateway-github/config"
)

// TestMainIsEntryPoint is a placeholder: main only delegates to loadConfig and run.
func TestMainIsEntryPoint(t *testing.T) {
	_ = main
}

// TestLoadConfigMissingUsesDefaults falls back to built-in defaults for a missing file.
func TestLoadConfigMissingUsesDefaults(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("addr = %q, want default :8080", cfg.Addr)
	}
}

// TestRunRejectsBadConfig checks run returns an error when the listen address is invalid.
func TestRunRejectsBadConfig(t *testing.T) {
	cfg := gwconfig.Default()
	cfg.Addr = "!!!not-a-valid-addr"
	if err := run(cfg); err == nil {
		t.Fatal("expected run to fail on an invalid listen address")
	}
}
