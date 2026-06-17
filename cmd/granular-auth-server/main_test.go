package main

import (
	"os"
	"path/filepath"
	"testing"

	asconfig "github.com/clems4ever/granular/auth_server/config"
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
	if cfg.Addr != ":9090" {
		t.Fatalf("addr = %q, want default :9090", cfg.Addr)
	}
}

// TestRunRejectsBadWorkspace checks run fails when the workspace directory cannot be created.
func TestRunRejectsBadWorkspace(t *testing.T) {
	// A workspace path nested under a regular file cannot be created.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := asconfig.Default()
	cfg.Workspace = filepath.Join(file, "sub")
	if err := run(cfg); err == nil {
		t.Fatal("expected run to fail creating the workspace")
	}
}
