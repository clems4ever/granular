// Package rscli builds a complete command-line client for a single granular
// resource server (RS). It supplies the commands every RS client needs — a
// `catalog` command that prints the RS's permission schema and a `sign` command
// that builds and has the RS sign a grant request — and lets the RS author add
// declarative, typed sub-commands for the RS's own operations. The signed grant
// request a `sign` produces is handed off to the granular CLI's `propose`
// command for human approval; this CLI never talks to the authorization server.
//
// Configuration is minimal: a small YAML file (default ~/.granular/<rsid>.yaml)
// holds only the RS's base URL, so an agent need not pass it on every call. The
// subject token is read from disk (DefaultSubjectTokenPath, ~/.granular/
// subject_token) — where an llmbox injects it. Flags (--base-url, --token,
// --token-file, --config) override both; catalog and sign need no token.
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

	"gopkg.in/yaml.v3"
)

// DefaultSubjectTokenPath is where the subject token is read from when no token
// is supplied by flags. It matches the path an llmbox injects the token to
// (~/.granular/subject_token).
const DefaultSubjectTokenPath = "~/.granular/subject_token"

// defaultConfigPath returns the default config file path for resource server
// rsID (~/.granular/<rsID>.yaml).
//
// @arg rsID The resource server id.
// @return string The default config file path.
//
// @testcase TestLoadConfigReadsBaseURL uses a config path for a resource server.
func defaultConfigPath(rsID string) string {
	return "~/.granular/" + rsID + ".yaml"
}

// fileConfig is the resource-server CLI config: only the RS base URL.
type fileConfig struct {
	BaseURL string `yaml:"base_url"`
}

// loadConfig reads and parses the config at path. A missing file is not an error:
// it yields an empty config so flags and the spec default can supply the base URL.
//
// @arg path The config file path ("~" is expanded).
// @return *fileConfig The parsed config, or an empty one when the file is absent.
// @error error when the file exists but cannot be parsed.
//
// @testcase TestLoadConfigMissingIsEmpty returns an empty config for a missing file.
// @testcase TestLoadConfigReadsBaseURL parses the base URL.
func loadConfig(path string) (*fileConfig, error) {
	data, err := os.ReadFile(expandHome(path))
	if os.IsNotExist(err) {
		return &fileConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c fileConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, nil
}

// resolveToken determines the subject token: the override (--token) wins;
// otherwise the token is read from tokenFile (with "~" expanded). A missing token
// file is tolerated (empty token) only when it is the default — tokenFileExplicit
// reports whether the path came from a flag, in which case a missing file is an
// error. Token-less commands (catalog, sign) work with an empty token.
//
// @arg tokenOverride The --token value, or "" if unset.
// @arg tokenFile The token file path (default or chosen).
// @arg tokenFileExplicit Whether the path came from a flag.
// @return string The resolved token, or "" when none is available.
// @error error when an explicitly chosen token file cannot be read, or any token file fails for a reason other than not existing.
//
// @testcase TestResolveTokenFlagOverrides returns the override without reading a file.
// @testcase TestResolveTokenReadsFile reads and trims the token from the file.
// @testcase TestResolveTokenMissingDefaultTolerated returns "" for a missing default file.
// @testcase TestResolveTokenExplicitMissingErrors errors for a missing explicit file.
func resolveToken(tokenOverride, tokenFile string, tokenFileExplicit bool) (string, error) {
	if tokenOverride != "" {
		return tokenOverride, nil
	}
	if tokenFile == "" {
		return "", nil
	}
	data, err := os.ReadFile(expandHome(tokenFile))
	if err != nil {
		if os.IsNotExist(err) && !tokenFileExplicit {
			return "", nil
		}
		return "", fmt.Errorf("token file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// expandHome replaces a leading "~" in path with the user's home directory,
// leaving the path unchanged when there is no "~" prefix or no home directory.
//
// @arg path The path to expand.
// @return string The path with a leading "~" expanded to $HOME.
//
// @testcase TestResolveTokenReadsFile relies on a plain (non-~) path round-tripping.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
