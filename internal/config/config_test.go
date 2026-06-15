package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeConfig writes content to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "granular.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadParsesYAML checks a full configuration file, including its secret files, is parsed.
func TestLoadParsesYAML(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	secretFile := filepath.Join(dir, "client_secret")
	sessionFile := filepath.Join(dir, "session")
	// A trailing newline (common in mounted secrets) must be trimmed.
	if err := os.WriteFile(tokenFile, []byte("ghp_token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretFile, []byte("oauth-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionFile, []byte("hmac-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	path := writeConfig(t, fmt.Sprintf(`
addr: ":9000"
base_url: "https://granular.example.com"
workspace: "/data"
db: "/data/g.db"
cleanup_interval: "5m"
github_token_file: %q
auth:
  client_id: "cid"
  client_secret_file: %q
  allowed_users:
    - clems4ever
    - alice
  session_secret_file: %q
`, tokenFile, secretFile, sessionFile))
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != ":9000" || c.BaseURL != "https://granular.example.com" {
		t.Fatalf("unexpected addr/base_url: %+v", c)
	}
	if c.DBPath != "/data/g.db" || c.CleanupInterval.Std() != 5*time.Minute {
		t.Fatalf("unexpected db/cleanup: %+v", c)
	}
	// Secrets are resolved from their files (and trimmed); client id stays inline.
	if c.GitHubToken != "ghp_token" || c.Auth.ClientID != "cid" || c.Auth.ClientSecret != "oauth-secret" || c.Auth.SessionSecret != "hmac-key" {
		t.Fatalf("unexpected resolved secrets: %+v", c)
	}
	if len(c.Auth.AllowedUsers) != 2 || c.Auth.AllowedUsers[0] != "clems4ever" {
		t.Fatalf("unexpected allowed_users: %+v", c.Auth.AllowedUsers)
	}
}

// TestLoadMissingSecretFile checks Load errors when a referenced secret file is absent.
func TestLoadMissingSecretFile(t *testing.T) {
	path := writeConfig(t, "github_token_file: /no/such/secret\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error when a secret file is missing")
	}
}

// TestLoadAppliesDefaults checks omitted fields fall back to derived defaults.
func TestLoadAppliesDefaults(t *testing.T) {
	path := writeConfig(t, "workspace: /srv/granular\n")
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != ":8080" {
		t.Fatalf("addr default = %q", c.Addr)
	}
	if c.BaseURL != "http://localhost:8080" {
		t.Fatalf("base_url should derive from addr, got %q", c.BaseURL)
	}
	if c.DBPath != filepath.Join("/srv/granular", "granular.db") {
		t.Fatalf("db should derive from workspace, got %q", c.DBPath)
	}
	if c.CleanupInterval.Std() != 30*time.Second {
		t.Fatalf("cleanup default = %v", c.CleanupInterval.Std())
	}
}

// TestDefault checks the built-in defaults used when no file is supplied.
func TestDefault(t *testing.T) {
	c := Default()
	if c.Addr != ":8080" || c.BaseURL != "http://localhost:8080" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.CleanupInterval.Std() != 30*time.Second {
		t.Fatalf("cleanup default = %v", c.CleanupInterval.Std())
	}
	if c.DBPath == "" || c.Workspace == "" {
		t.Fatal("workspace/db defaults should be set")
	}
}

// TestLoadMissingFile checks a missing file yields an os.ErrNotExist error.
func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "absent.yaml"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

// TestLoadInvalidYAML checks malformed YAML returns an error.
func TestLoadInvalidYAML(t *testing.T) {
	path := writeConfig(t, "addr: \":8080\"\n\tbad: : :\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

// TestDurationUnmarshalInvalid checks an invalid duration string is rejected.
func TestDurationUnmarshalInvalid(t *testing.T) {
	path := writeConfig(t, "cleanup_interval: \"not-a-duration\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an invalid duration")
	}
}

// TestDurationStd checks Std returns the underlying time.Duration.
func TestDurationStd(t *testing.T) {
	if Duration(90*time.Second).Std() != 90*time.Second {
		t.Fatal("Std should round-trip the duration")
	}
}
