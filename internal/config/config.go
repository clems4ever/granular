// Package config loads the granular server configuration from a YAML file,
// applying built-in defaults for any field that is omitted. It is the single
// source of truth for server settings, replacing the previous environment
// variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the granular server configuration. Every field is optional in the
// YAML file; omitted fields fall back to their default (see applyDefaults).
//
// Secrets are never stored in the file itself: the file names a path to a file
// holding each secret, and Load reads it into the matching resolved field
// (GitHubToken), which is not part of the YAML schema.
type Config struct {
	Addr            string   `yaml:"addr"`
	BaseURL         string   `yaml:"base_url"`
	Workspace       string   `yaml:"workspace"`
	DBPath          string   `yaml:"db"`
	CleanupInterval Duration `yaml:"cleanup_interval"`
	// GitHubTokenFile is the path to a file holding the GitHub PAT used for
	// github.* operations and the git proxy.
	GitHubTokenFile string `yaml:"github_token_file"`
	Auth            Auth   `yaml:"auth"`

	// GitHubToken is the PAT read from GitHubTokenFile at load time.
	GitHubToken string `yaml:"-"`
}

// Auth holds the web-UI authentication settings. When ClientID and ClientSecret
// are both set, the human pages require a GitHub login limited to AllowedUsers.
// The two secrets are loaded from the *_file paths, never stored inline.
type Auth struct {
	ClientID          string   `yaml:"client_id"`
	ClientSecretFile  string   `yaml:"client_secret_file"`
	AllowedUsers      []string `yaml:"allowed_users"`
	SessionSecretFile string   `yaml:"session_secret_file"`

	// ClientSecret and SessionSecret are read from their *_file paths at load time.
	ClientSecret  string `yaml:"-"`
	SessionSecret string `yaml:"-"`
}

// Duration is a time.Duration that unmarshals from a YAML string such as "30s".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string (e.g. "2m") into a Duration.
//
// @arg value The YAML node holding the duration string.
// @error error when the node is not a string or not a valid Go duration.
//
// @testcase TestLoadParsesYAML parses a duration from a config file.
// @testcase TestDurationUnmarshalInvalid rejects a malformed duration.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Std returns the value as a standard time.Duration.
//
// @return time.Duration The duration value.
//
// @testcase TestDurationStd converts a Duration back to time.Duration.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

// Load reads and parses the YAML configuration file at path, fills any omitted
// field with its default, and reads each configured secret file into its resolved
// field. Callers may test the returned error against os.ErrNotExist to fall back
// to Default when no file is present.
//
// @arg path The path to the YAML configuration file.
// @return *Config The parsed configuration with defaults and secrets resolved.
// @error error when the file cannot be read, is not valid YAML, or a referenced secret file cannot be read.
//
// @testcase TestLoadParsesYAML loads a full configuration file and its secret files.
// @testcase TestLoadAppliesDefaults fills omitted fields.
// @testcase TestLoadMissingFile returns os.ErrNotExist for a missing file.
// @testcase TestLoadInvalidYAML returns an error for malformed YAML.
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

// resolveSecrets reads each configured secret file into its resolved field. A
// secret whose path is empty resolves to the empty string; a path that cannot be
// read is a fatal error.
//
// @error error when a referenced secret file cannot be read.
//
// @testcase TestLoadParsesYAML resolves the secret files of a config.
// @testcase TestLoadMissingSecretFile errors on an unreadable secret file.
func (c *Config) resolveSecrets() error {
	var err error
	if c.GitHubToken, err = readSecretFile(c.GitHubTokenFile); err != nil {
		return fmt.Errorf("github_token_file: %w", err)
	}
	if c.Auth.ClientSecret, err = readSecretFile(c.Auth.ClientSecretFile); err != nil {
		return fmt.Errorf("auth.client_secret_file: %w", err)
	}
	if c.Auth.SessionSecret, err = readSecretFile(c.Auth.SessionSecretFile); err != nil {
		return fmt.Errorf("auth.session_secret_file: %w", err)
	}
	return nil
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
// holds its built-in default.
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
		c.Addr = ":8080"
	}
	if c.BaseURL == "" {
		c.BaseURL = "http://localhost" + c.Addr
	}
	if c.Workspace == "" {
		c.Workspace = filepath.Join(os.TempDir(), "granular-workspace")
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.Workspace, "granular.db")
	}
	if c.CleanupInterval == 0 {
		c.CleanupInterval = Duration(30 * time.Second)
	}
}
