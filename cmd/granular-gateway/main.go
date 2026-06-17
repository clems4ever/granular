// Command granular-gateway is the granular gateway (Resource Server): it holds the
// platform credential and the permission vocabulary. It serves the permission schema,
// signs grant requests for clients, and executes operations only after the
// authorization server (AS) confirms they are authorized.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/clems4ever/granular/gateway/asclient"
	gwconfig "github.com/clems4ever/granular/gateway/config"
	"github.com/clems4ever/granular/gateway/server"
	"github.com/clems4ever/granular/internal/operations"
	githubops "github.com/clems4ever/granular/internal/operations/github"
)

// main parses flags, loads the YAML configuration, and starts the gateway.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only delegates to run.
func main() {
	configPath := flag.String("config", "granular-gateway.yaml", "path to the YAML configuration file")
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// loadConfig loads the configuration file, falling back to built-in defaults when the
// file does not exist.
//
// @arg path The path to the YAML configuration file.
// @return *gwconfig.Config The loaded or default configuration.
// @error error when the file exists but cannot be read or parsed.
//
// @testcase TestLoadConfigMissingUsesDefaults falls back to defaults for a missing file.
func loadConfig(path string) (*gwconfig.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		log.Printf("config file %q not found; using built-in defaults", path)
		return gwconfig.Default(), nil
	}
	cfg, err := gwconfig.Load(path)
	if err != nil {
		return nil, err
	}
	log.Printf("loaded configuration from %q", path)
	return cfg, nil
}

// run builds the gateway from cfg and serves until the process is stopped.
//
// @arg cfg The gateway configuration.
// @error error Any error from ListenAndServe.
//
// @testcase TestRunRejectsBadConfig is a placeholder for run wiring.
func run(cfg *gwconfig.Config) error {
	env := operations.Env{GitHubToken: cfg.GitHubToken, BaseURL: cfg.BaseURL}

	registry := operations.NewRegistry()
	registry.Register(githubops.TypeClone, githubops.Clone)
	registry.Register(githubops.TypeIssueList, githubops.IssueList)
	registry.Register(githubops.TypeIssueView, githubops.IssueView)
	registry.Register(githubops.TypeIssueComment, githubops.IssueComment)
	registry.Register(githubops.TypeIssueCreate, githubops.IssueCreate)
	registry.Register(githubops.TypeIssueEdit, githubops.IssueEdit)
	registry.Register(githubops.TypeIssueClose, githubops.IssueClose)
	registry.Register(githubops.TypeIssueReopen, githubops.IssueReopen)
	registry.Register(githubops.TypePush, githubops.Push)
	registry.Register(githubops.TypePullList, githubops.PullList)
	registry.Register(githubops.TypePullView, githubops.PullView)
	registry.Register(githubops.TypePullDiff, githubops.PullDiff)
	registry.Register(githubops.TypePullCreate, githubops.PullCreate)
	registry.Register(githubops.TypePullComment, githubops.PullComment)
	registry.Register(githubops.TypePullReview, githubops.PullReview)
	registry.Register(githubops.TypePullEdit, githubops.PullEdit)
	registry.Register(githubops.TypePullMerge, githubops.PullMerge)
	registry.Register(githubops.TypePullClose, githubops.PullClose)
	registry.Register(githubops.TypePullReopen, githubops.PullReopen)

	if cfg.Secret == "" {
		log.Printf("warning: secret is empty; the AS will reject this gateway's signatures and verify calls until secret_file is set")
	}
	if cfg.GitHubToken == "" {
		log.Printf("warning: github_token is empty; github.* operations cannot authenticate until it is set")
	}

	verifier := asclient.New(cfg.ASURL, cfg.GatewayID, []byte(cfg.Secret))
	srv := server.New(cfg.GatewayID, []byte(cfg.Secret), registry, env, verifier)

	log.Printf("granular-gateway %q listening on %s (base URL %s, AS %s)", cfg.GatewayID, cfg.Addr, cfg.BaseURL, cfg.ASURL)
	return fmt.Errorf("server stopped: %w", http.ListenAndServe(cfg.Addr, srv.Handler()))
}
