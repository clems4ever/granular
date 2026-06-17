package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/clems4ever/granular/client"
)

// gatewayConf is one gateway entry in the client configuration file.
type gatewayConf struct {
	ID      string `yaml:"id"`
	BaseURL string `yaml:"base_url"`
}

// Config is the granular client configuration: the AS base URL, an optional file holding
// the policy token, and the known gateways. The token is never stored inline — token_file
// names a path read at load time.
type Config struct {
	ASURL     string        `yaml:"as_url"`
	TokenFile string        `yaml:"token_file"`
	Gateways  []gatewayConf `yaml:"gateways"`

	// Token is read from TokenFile at load time (empty when no file is set).
	Token string `yaml:"-"`
}

// Load reads and parses the YAML configuration at path, applies defaults, and reads the
// token file into Token when one is configured.
//
// @arg path The path to the YAML configuration file.
// @return *Config The parsed configuration with its token resolved.
// @error error when the file cannot be read, is invalid YAML, or the token file cannot be read.
//
// @testcase TestLoadParsesConfig loads gateways and resolves the token file.
// @testcase TestLoadMissingTokenFile errors when the token file is absent.
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
	if c.TokenFile != "" {
		tok, err := os.ReadFile(c.TokenFile)
		if err != nil {
			return nil, fmt.Errorf("token_file: %w", err)
		}
		c.Token = strings.TrimSpace(string(tok))
	}
	return &c, nil
}

// Default returns the configuration used when no file is supplied.
//
// @return *Config A configuration with defaults applied and no gateways.
//
// @testcase TestDefaultConfig checks the built-in defaults.
func Default() *Config {
	var c Config
	c.applyDefaults()
	return &c
}

// applyDefaults fills any unset field with its default.
//
// @testcase TestDefaultConfig checks the standalone defaults.
func (c *Config) applyDefaults() {
	if c.ASURL == "" {
		c.ASURL = "http://localhost:9090"
	}
}

// toClient builds an SDK client from the configuration, overriding the resolved token
// with tokenOverride when it is non-empty (e.g. from a flag or environment variable).
//
// @arg tokenOverride A token that takes precedence over the configured one, or "".
// @return *client.Client The configured SDK client.
//
// @testcase TestToClientUsesOverride prefers the override token.
func (c *Config) toClient(tokenOverride string) *client.Client {
	token := c.Token
	if tokenOverride != "" {
		token = tokenOverride
	}
	gws := make([]client.Gateway, len(c.Gateways))
	for i, g := range c.Gateways {
		gws[i] = client.Gateway{ID: g.ID, BaseURL: g.BaseURL}
	}
	return client.New(client.Config{ASURL: c.ASURL, Token: token, Gateways: gws})
}
