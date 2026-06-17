package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadParsesYAML loads a full configuration file with gateway and auth secret
// files and checks every field, including the resolved secrets.
func TestLoadParsesYAML(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "gw.secret")
	if err := os.WriteFile(secret, []byte("  s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adminTok := filepath.Join(dir, "admin.token")
	if err := os.WriteFile(adminTok, []byte("  admintok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "as.yaml")
	body := "addr: \":7000\"\n" +
		"base_url: \"http://as.example\"\n" +
		"admin_token_file: " + adminTok + "\n" +
		"gateways:\n" +
		"  - id: github-gateway\n" +
		"    secret_file: " + secret + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":7000" || cfg.BaseURL != "http://as.example" {
		t.Fatalf("unexpected addr/base: %q %q", cfg.Addr, cfg.BaseURL)
	}
	if got := cfg.GatewaySecrets()["github-gateway"]; got != "s3cret" {
		t.Fatalf("gateway secret = %q, want trimmed s3cret", got)
	}
	if cfg.AdminToken != "admintok" {
		t.Fatalf("admin token = %q, want trimmed admintok", cfg.AdminToken)
	}
}

// TestLoadAppliesDefaults checks omitted fields fall back to their defaults.
func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "as.yaml")
	if err := os.WriteFile(cfgPath, []byte("addr: \":1234\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BaseURL != "http://localhost:1234" {
		t.Fatalf("derived base url = %q", cfg.BaseURL)
	}
	if cfg.DBPath == "" || cfg.CleanupInterval == 0 {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.RequestTTL.Std() != 15*time.Minute {
		t.Fatalf("default request TTL = %s, want 15m", cfg.RequestTTL.Std())
	}
}

// TestLoadMissingFile returns an os.ErrNotExist-compatible error for a missing file.
func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); !os.IsNotExist(err) {
		t.Fatalf("err = %v, want not-exist", err)
	}
}

// TestLoadMissingSecretFile errors when a referenced secret file is absent.
func TestLoadMissingSecretFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "as.yaml")
	body := "gateways:\n  - id: gw\n    secret_file: " + filepath.Join(dir, "absent") + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("expected error for missing secret file")
	}
}

// TestDefault checks the standalone defaults.
func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Addr != ":9090" || cfg.DBPath == "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

// TestGatewaySecretsSkipsIncomplete drops entries missing an id or secret.
func TestGatewaySecretsSkipsIncomplete(t *testing.T) {
	cfg := &Config{Gateways: []Gateway{
		{ID: "a", Secret: "x"},
		{ID: "", Secret: "y"},
		{ID: "b", Secret: ""},
	}}
	got := cfg.GatewaySecrets()
	if len(got) != 1 || got["a"] != "x" {
		t.Fatalf("got %v, want only a", got)
	}
}
