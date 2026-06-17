// Package rscli builds a complete command-line client for a single granular
// resource server (RS). It supplies the commands every RS client needs — a
// `catalog` command that prints the RS's permission schema and a `sign` command
// that builds and has the RS sign a grant request — and lets the RS author add
// declarative, typed sub-commands for the RS's own operations. The signed grant
// request a `sign` produces is handed off to the granular CLI's `propose`
// command for human approval; this CLI never talks to the authorization server.
//
// An RS author builds a CLI by describing it with a Spec and calling NewRootCmd:
//
//	root := rscli.NewRootCmd(rscli.Spec{
//	    Use: "granular-github", RSID: "github", DefaultBaseURL: "http://localhost:9091",
//	    Operations: []rscli.OpCommand{
//	        {Path: "clone", Type: "github.clone", Flags: []rscli.Flag{{Name: "repo", Required: true}}},
//	        // ...
//	    },
//	}, os.Stdout)
//	root.Execute()
package rscli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/clems4ever/granular/client"
	"gopkg.in/yaml.v3"
)

// Config is a resource-server CLI's configuration: the RS's HTTP base URL and the
// file holding the subject token operations run under. catalog and sign need no
// token; only running operations does.
type Config struct {
	// BaseURL is the resource server's HTTP base URL.
	BaseURL string `yaml:"base_url"`
	// TokenFile is the path to the subject token file; "~" is expanded to $HOME.
	TokenFile string `yaml:"token_file"`

	// Token is read from TokenFile at load time (empty when no file is set).
	Token string `yaml:"-"`
}

// Load reads and parses the YAML configuration at path. A missing file is not an
// error: it returns an empty Config so flags and defaults can supply everything.
// When TokenFile is set, the token is read from it (with "~" expanded) and
// trimmed.
//
// @arg path The configuration file path.
// @return *Config The parsed configuration, or an empty one when the file is absent.
// @error error when the file exists but cannot be parsed, or the token file cannot be read.
//
// @testcase TestLoadMissingFileIsEmpty returns an empty config when the file is absent.
// @testcase TestLoadReadsTokenFile parses base_url and reads the token file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.TokenFile != "" {
		tok, err := os.ReadFile(expandHome(c.TokenFile))
		if err != nil {
			return nil, fmt.Errorf("token_file: %w", err)
		}
		c.Token = strings.TrimSpace(string(tok))
	}
	return &c, nil
}

// toClient builds an SDK client targeting the one resource server rsID. The base
// URL and token come from the config, each overridable by a non-empty argument
// (from a CLI flag). The AS URL is left empty: catalog, sign, and run never call
// the authorization server.
//
// @arg rsID The resource server id this CLI targets.
// @arg baseURLOverride A base URL that, when non-empty, replaces the configured one.
// @arg tokenOverride A subject token that, when non-empty, replaces the configured one.
// @return *client.Client A client configured for the single resource server.
//
// @testcase TestToClientAppliesOverrides prefers the override base URL and token.
func (c *Config) toClient(rsID, baseURLOverride, tokenOverride string) *client.Client {
	base := c.BaseURL
	if baseURLOverride != "" {
		base = baseURLOverride
	}
	token := c.Token
	if tokenOverride != "" {
		token = tokenOverride
	}
	return client.New(client.Config{
		Token:           token,
		ResourceServers: []client.ResourceServer{{ID: rsID, BaseURL: base}},
	})
}

// expandHome replaces a leading "~" in path with the user's home directory,
// leaving the path unchanged when there is no "~" prefix or no home directory.
//
// @arg path The path to expand.
// @return string The path with a leading "~" expanded to $HOME.
//
// @testcase TestLoadReadsTokenFile relies on a plain (non-~) path round-tripping.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
