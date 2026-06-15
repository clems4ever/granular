// Command granular-server is the HTTP server. It holds platform credentials,
// checks grants, mints grant requests, serves the approval page, and
// executes approved operations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/clems4ever/granular/internal/config"
	"github.com/clems4ever/granular/internal/grants"
	"github.com/clems4ever/granular/internal/operations"
	githubops "github.com/clems4ever/granular/internal/operations/github"
	"github.com/clems4ever/granular/internal/server"
)

// main parses flags, loads the YAML configuration, and starts the server.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only delegates to run.
func main() {
	configPath := flag.String("config", "granular.yaml", "path to the YAML configuration file")
	flag.Parse()
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

// loadConfig loads the configuration file, falling back to built-in defaults when
// the file does not exist, so the server runs out of the box without one.
//
// @arg path The path to the YAML configuration file.
// @return *config.Config The loaded or default configuration.
// @error error when the file exists but cannot be read or parsed.
//
// @testcase TestLoadConfigMissingUsesDefaults falls back to defaults for a missing file.
func loadConfig(path string) (*config.Config, error) {
	// Decide the fallback by the config file's own existence, so that a missing
	// *secret* file referenced inside it stays a fatal error rather than silently
	// dropping us onto the defaults.
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		log.Printf("config file %q not found; using built-in defaults", path)
		return config.Default(), nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	log.Printf("loaded configuration from %q", path)
	return cfg, nil
}

// run builds the server from cfg and serves until the process is stopped.
//
// @arg cfg The server configuration.
// @error error Any error from configuration or from ListenAndServe.
//
// @testcase TestRunRejectsBadWorkspace checks run fails when the workspace cannot be created.
func run(cfg *config.Config) error {
	if err := os.MkdirAll(cfg.Workspace, 0o755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	store, err := grants.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	env := operations.Env{
		GitHubToken: cfg.GitHubToken,
		BaseURL:     cfg.BaseURL,
	}

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

	srv := server.New(registry, store, env, cfg.BaseURL)

	auth := server.NewAuthenticator(server.AuthConfig{
		ClientID:      cfg.Auth.ClientID,
		ClientSecret:  cfg.Auth.ClientSecret,
		AllowedUsers:  cfg.Auth.AllowedUsers,
		SessionSecret: []byte(cfg.Auth.SessionSecret),
		BaseURL:       cfg.BaseURL,
	})
	srv.UseAuth(auth)
	switch {
	case !auth.Enabled():
		log.Printf("warning: web UI is UNAUTHENTICATED; set auth.client_id and auth.client_secret_file to require a GitHub login (OAuth app callback URL: %s/auth/callback)", cfg.BaseURL)
	case auth.AllowedCount() == 0:
		log.Printf("warning: web UI authentication is enabled but auth.allowed_users is empty; all logins will be denied")
	default:
		log.Printf("web UI authentication enabled (GitHub OAuth); %d allowed user(s)", auth.AllowedCount())
	}

	store.StartCleanup(context.Background(), cfg.CleanupInterval.Std(), func(n int) {
		log.Printf("cleaned up %d expired grant(s)", n)
	})
	log.Printf("grant janitor purging expired grants every %s", cfg.CleanupInterval.Std())

	log.Printf("granular-server listening on %s (base URL %s, workspace %s)", cfg.Addr, cfg.BaseURL, cfg.Workspace)
	if env.GitHubToken == "" {
		log.Printf("warning: github_token is empty; the git proxy cannot authenticate to GitHub until it is set")
	}
	return http.ListenAndServe(cfg.Addr, srv.Handler())
}
