package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/clems4ever/granular/internal/config"
)

// TestLoadConfigMissingUsesDefaults checks loadConfig falls back to defaults when
// the configuration file does not exist.
func TestLoadConfigMissingUsesDefaults(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "absent.yaml"))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("expected built-in defaults, got addr %q", cfg.Addr)
	}
}

// TestLoadConfigReadsFile checks loadConfig parses an existing configuration file.
func TestLoadConfigReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "granular.yaml")
	if err := os.WriteFile(path, []byte("addr: \":9999\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Addr != ":9999" {
		t.Fatalf("addr = %q, want :9999", cfg.Addr)
	}
}

// TestLoadConfigPropagatesSecretError checks that when the config file exists but
// references a missing secret file, loadConfig returns an error instead of
// silently falling back to defaults (the bug: a missing secret file matched
// os.ErrNotExist and was mistaken for a missing config file).
func TestLoadConfigPropagatesSecretError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "granular.yaml")
	if err := os.WriteFile(path, []byte("github_token_file: /no/such/secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err == nil {
		t.Fatalf("expected an error for a missing secret file, got config %+v", cfg)
	}
}

// TestRunRejectsBadWorkspace checks run fails fast when the workspace directory
// cannot be created.
func TestRunRejectsBadWorkspace(t *testing.T) {
	// Point the workspace at a path whose parent is a regular file, so the
	// MkdirAll in run fails fast before any server is started.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Workspace = filepath.Join(file, "workspace")
	cfg.DBPath = filepath.Join(cfg.Workspace, "granular.db")
	if err := run(cfg); err == nil {
		t.Fatal("run should fail when the workspace cannot be created")
	}
}

// TestMainIsEntryPoint is a placeholder: main only wires log.Fatal to run and
// cannot be invoked in-process without terminating the test binary, so the
// delegation it performs is exercised through TestRunRejectsBadWorkspace.
func TestMainIsEntryPoint(t *testing.T) {
	if testing.Short() {
		t.Skip("main is an entry point exercised via run")
	}
}
