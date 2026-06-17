package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefault checks the built-in defaults.
func TestDefault(t *testing.T) {
	c := Default()
	if c.Addr != ":8080" || c.GatewayID != "github-gateway" || c.ASURL != "http://localhost:9090" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

// TestLoadParsesYAML loads a full configuration and resolves its secret files.
func TestLoadParsesYAML(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "gw.secret")
	tok := filepath.Join(dir, "pat")
	if err := os.WriteFile(secret, []byte("  s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte("ghp_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "gw.yaml")
	body := "gateway_id: gw1\nas_url: http://as:9090\nsecret_file: " + secret + "\ngithub_token_file: " + tok + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.GatewayID != "gw1" || c.ASURL != "http://as:9090" {
		t.Fatalf("unexpected fields: %+v", c)
	}
	if c.Secret != "s3cret" || c.GitHubToken != "ghp_x" {
		t.Fatalf("secrets not resolved/trimmed: secret=%q token=%q", c.Secret, c.GitHubToken)
	}
}

// TestLoadMissingSecretFile errors when a referenced secret file is absent.
func TestLoadMissingSecretFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gw.yaml")
	body := "secret_file: " + filepath.Join(dir, "absent") + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("expected error for a missing secret file")
	}
}
