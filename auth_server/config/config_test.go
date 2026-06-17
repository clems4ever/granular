package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLoadParsesYAML loads a full configuration file with resource server and auth secret
// files and checks every field, including the resolved secrets.
func TestLoadParsesYAML(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "rs.secret")
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
		"grant_request_ttl: \"30m\"\n" +
		"admin_token_file: " + adminTok + "\n" +
		"resource_servers:\n" +
		"  - id: github-resource-server\n" +
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
	if cfg.GrantRequestTTL.Std() != 30*time.Minute {
		t.Fatalf("grant-request TTL = %s, want parsed 30m", cfg.GrantRequestTTL.Std())
	}
	if got := cfg.ResourceServerSecrets()["github-resource-server"]; got != "s3cret" {
		t.Fatalf("resource server secret = %q, want trimmed s3cret", got)
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
	if cfg.GrantRequestTTL.Std() != 15*time.Minute {
		t.Fatalf("default grant-request TTL = %s, want 15m", cfg.GrantRequestTTL.Std())
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
	body := "resource_servers:\n  - id: rs\n    secret_file: " + filepath.Join(dir, "absent") + "\n"
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

// TestResourceServerSecretsSkipsIncomplete drops entries missing an id or secret.
func TestResourceServerSecretsSkipsIncomplete(t *testing.T) {
	cfg := &Config{ResourceServers: []ResourceServer{
		{ID: "a", Secret: "x"},
		{ID: "", Secret: "y"},
		{ID: "b", Secret: ""},
	}}
	got := cfg.ResourceServerSecrets()
	if len(got) != 1 || got["a"] != "x" {
		t.Fatalf("got %v, want only a", got)
	}
}

// TestDurationUnmarshalInvalid checks an invalid duration string is rejected.
func TestDurationUnmarshalInvalid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "as.yaml")
	if err := os.WriteFile(cfgPath, []byte("cleanup_interval: \"not-a-duration\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("expected an error for an invalid duration")
	}
}

// TestDurationStd checks Std returns the underlying time.Duration.
func TestDurationStd(t *testing.T) {
	if Duration(90*time.Second).Std() != 90*time.Second {
		t.Fatal("Std should round-trip the duration")
	}
}
