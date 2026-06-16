// Package config loads the granular authorization-server (AS) configuration from a
// YAML file, applying built-in defaults for any omitted field. The AS is the policy
// authority: it stores grants, runs the human consent screen, and answers
// allow/deny. It does not hold any platform credential — those live on the
// Gateways, which authenticate to the AS with a per-gateway shared secret registered
// here.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/clems4ever/granular/internal/config"
	"gopkg.in/yaml.v3"
)

// Config is the authorization-server configuration. Every field is optional in the
// YAML file; omitted fields fall back to their default (see applyDefaults).
//
// Secrets are never stored inline: each *_file key names a path to a file holding
// the secret, which Load reads into the matching resolved field.
type Config struct {
	Addr            string          `yaml:"addr"`
	BaseURL         string          `yaml:"base_url"`
	Workspace       string          `yaml:"workspace"`
	DBPath          string          `yaml:"db"`
	CleanupInterval config.Duration `yaml:"cleanup_interval"`

	// Gateways registers the Resource-Gateways permitted to talk to the AS. Each
	// gateway authenticates its grant-request and verify calls with the shared
	// secret loaded from its secret_file.
	Gateways []Gateway `yaml:"gateways"`

	// Auth holds the GitHub-OAuth settings protecting the human consent screen.
	Auth Auth `yaml:"auth"`
}

// Gateway registers one Resource-Gateway and the shared secret it signs its
// requests with. The id is sent in the X-Gateway-ID header; the secret (loaded from
// SecretFile) keys the HMAC the AS verifies.
type Gateway struct {
	ID         string `yaml:"id"`
	SecretFile string `yaml:"secret_file"`

	// Secret is read from SecretFile at load time.
	Secret string `yaml:"-"`
}

// Auth holds the consent-screen authentication settings. When ClientID and
// ClientSecret are both set, the human pages require a "log in with GitHub". There is
// no global allowlist: a proposal names the approver's email, and only the human
// signed in with that email may decide it. The two secrets are loaded from the *_file
// paths, never inline.
type Auth struct {
	ClientID          string `yaml:"client_id"`
	ClientSecretFile  string `yaml:"client_secret_file"`
	SessionSecretFile string `yaml:"session_secret_file"`

	// ClientSecret and SessionSecret are read from their *_file paths at load time.
	ClientSecret  string `yaml:"-"`
	SessionSecret string `yaml:"-"`
}

// Load reads and parses the YAML configuration file at path, fills any omitted
// field with its default, and reads each configured secret file into its resolved
// field. Callers may test the returned error against os.ErrNotExist to fall back to
// Default when no file is present.
//
// @arg path The path to the YAML configuration file.
// @return *Config The parsed configuration with defaults and secrets resolved.
// @error error when the file cannot be read, is not valid YAML, or a referenced secret file cannot be read.
//
// @testcase TestLoadParsesYAML loads a full configuration file and its secret files.
// @testcase TestLoadAppliesDefaults fills omitted fields.
// @testcase TestLoadMissingFile returns os.ErrNotExist for a missing file.
// @testcase TestLoadMissingSecretFile errors when a secret file is missing.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.resolveSecrets(); err != nil {
		return nil, err
	}
	return &c, nil
}

// resolveSecrets reads each configured secret file (the gateway secrets and the
// OAuth/session secrets) into its resolved field. An empty path resolves to the
// empty string; a path that cannot be read is a fatal error.
//
// @error error when a referenced secret file cannot be read.
//
// @testcase TestLoadParsesYAML resolves the gateway and auth secret files.
// @testcase TestLoadMissingSecretFile errors on an unreadable secret file.
func (c *Config) resolveSecrets() error {
	var err error
	for i := range c.Gateways {
		if c.Gateways[i].Secret, err = readSecretFile(c.Gateways[i].SecretFile); err != nil {
			return fmt.Errorf("gateways[%d].secret_file: %w", i, err)
		}
	}
	if c.Auth.ClientSecret, err = readSecretFile(c.Auth.ClientSecretFile); err != nil {
		return fmt.Errorf("auth.client_secret_file: %w", err)
	}
	if c.Auth.SessionSecret, err = readSecretFile(c.Auth.SessionSecretFile); err != nil {
		return fmt.Errorf("auth.session_secret_file: %w", err)
	}
	return nil
}

// GatewaySecrets returns the map of registered gateway id to shared secret, used by
// the server to authenticate gateway requests. Gateways with an empty id or secret
// are skipped (they could never authenticate anyway).
//
// @return map[string]string The id→secret map of registered gateways.
//
// @testcase TestGatewaySecretsSkipsIncomplete drops entries missing an id or secret.
func (c *Config) GatewaySecrets() map[string]string {
	out := make(map[string]string, len(c.Gateways))
	for _, g := range c.Gateways {
		if g.ID != "" && g.Secret != "" {
			out[g.ID] = g.Secret
		}
	}
	return out
}

// readSecretFile returns the trimmed contents of the file at path, or the empty
// string when path is empty.
//
// @arg path The path to a file holding a secret, or "" for no secret.
// @return string The trimmed file contents, or "" when path is empty.
// @error error when path is set but the file cannot be read.
//
// @testcase TestLoadParsesYAML reads secret files referenced by a config.
// @testcase TestLoadMissingSecretFile errors when the path does not exist.
func readSecretFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// Default returns the configuration used when no file is supplied: every field
// holds its built-in default and no gateways are registered.
//
// @return *Config A configuration with all defaults applied.
//
// @testcase TestDefault checks the built-in defaults.
func Default() *Config {
	var c Config
	c.applyDefaults()
	return &c
}

// applyDefaults fills any unset field with its default, including values derived
// from other fields (the base URL from the address, the database path from the
// workspace).
//
// @testcase TestLoadAppliesDefaults exercises the derived defaults.
// @testcase TestDefault checks the standalone defaults.
func (c *Config) applyDefaults() {
	if c.Addr == "" {
		c.Addr = ":9090"
	}
	if c.BaseURL == "" {
		c.BaseURL = "http://localhost" + c.Addr
	}
	if c.Workspace == "" {
		c.Workspace = filepath.Join(os.TempDir(), "granular-auth-workspace")
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.Workspace, "granular-auth.db")
	}
	if c.CleanupInterval == 0 {
		c.CleanupInterval = config.Duration(30 * time.Second)
	}
}
