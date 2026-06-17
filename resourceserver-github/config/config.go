// Package config loads the granular GitHub resource server configuration from a YAML file,
// applying built-in defaults for any omitted field. It is the domain-specific
// configuration for the GitHub resource server built on the generic resource server SDK: it adds the
// GitHub credential the SDK itself knows nothing about. Secrets are never stored inline:
// each *_file key names a path to a file holding the secret, read into the matching
// resolved field at load time.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the GitHub resource server configuration.
type Config struct {
	Addr string `yaml:"addr"`

	// ResourceServerID identifies this resource server to the AS; ASURL is the AS base URL the resource server
	// calls to verify operations.
	ResourceServerID string `yaml:"resource_server_id"`
	ASURL            string `yaml:"as_url"`

	// SecretFile holds the HMAC secret shared with the AS (used to sign grant requests
	// and authenticate verify calls).
	SecretFile string `yaml:"secret_file"`

	// GitHubTokenFile holds the GitHub PAT used to execute github.* operations.
	GitHubTokenFile string `yaml:"github_token_file"`

	// Secret and GitHubToken are read from their *_file paths at load time.
	Secret      string `yaml:"-"`
	GitHubToken string `yaml:"-"`
}

// Load reads and parses the YAML configuration file at path, fills omitted fields with
// their defaults, and reads each configured secret file into its resolved field.
//
// @arg path The path to the YAML configuration file.
// @return *Config The parsed configuration with defaults and secrets resolved.
// @error error when the file cannot be read, is not valid YAML, or a secret file cannot be read.
//
// @testcase TestLoadParsesYAML loads a full configuration file and its secret files.
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

// resolveSecrets reads the resource server secret and GitHub token files into their resolved
// fields.
//
// @error error when a referenced secret file cannot be read.
//
// @testcase TestLoadParsesYAML resolves the secret files of a config.
// @testcase TestLoadMissingSecretFile errors on an unreadable secret file.
func (c *Config) resolveSecrets() error {
	var err error
	if c.Secret, err = readSecretFile(c.SecretFile); err != nil {
		return fmt.Errorf("secret_file: %w", err)
	}
	if c.GitHubToken, err = readSecretFile(c.GitHubTokenFile); err != nil {
		return fmt.Errorf("github_token_file: %w", err)
	}
	return nil
}

// readSecretFile returns the trimmed contents of the file at path, or "" when path is
// empty.
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

// Default returns the configuration used when no file is supplied.
//
// @return *Config A configuration with all defaults applied.
//
// @testcase TestDefault checks the built-in defaults.
func Default() *Config {
	var c Config
	c.applyDefaults()
	return &c
}

// applyDefaults fills any unset field with its default.
//
// @testcase TestDefault checks the standalone defaults.
func (c *Config) applyDefaults() {
	if c.Addr == "" {
		c.Addr = ":8080"
	}
	if c.ResourceServerID == "" {
		c.ResourceServerID = "github-resource-server"
	}
	if c.ASURL == "" {
		c.ASURL = "http://localhost:9090"
	}
}
