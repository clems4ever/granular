// Command granular-server is the HTTP server. It holds platform credentials,
// checks grants, mints grant requests, serves the approval page, and
// executes approved operations.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/clems4ever/granular/internal/grants"
	"github.com/clems4ever/granular/internal/operations"
	githubops "github.com/clems4ever/granular/internal/operations/github"
	"github.com/clems4ever/granular/internal/server"
)

// main configures and starts the granular HTTP server, reading its settings from
// the environment.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only delegates to run.
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// run builds the server from environment configuration and serves until the
// process is stopped.
//
// @error error Any error from configuration or from ListenAndServe.
//
// @testcase TestRunRejectsBadWorkspace is a placeholder for config validation tests.
func run() error {
	addr := envOr("GRANULAR_ADDR", ":8080")
	baseURL := envOr("GRANULAR_BASE_URL", "http://localhost"+addr)
	workspace := envOr("GRANULAR_WORKSPACE", filepath.Join(os.TempDir(), "granular-workspace"))
	dbPath := envOr("GRANULAR_DB", filepath.Join(workspace, "granular.db"))

	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	store, err := grants.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	env := operations.Env{
		GitHubToken: os.Getenv("GRANULAR_GITHUB_TOKEN"),
		BaseURL:     baseURL,
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

	srv := server.New(registry, store, env, baseURL)

	cleanupInterval := parseDurationOr("GRANULAR_CLEANUP_INTERVAL", 30*time.Second)
	store.StartCleanup(context.Background(), cleanupInterval, func(n int) {
		log.Printf("cleaned up %d expired grant(s)", n)
	})
	log.Printf("grant janitor purging expired grants every %s", cleanupInterval)

	log.Printf("granular-server listening on %s (base URL %s, workspace %s)", addr, baseURL, workspace)
	if env.GitHubToken == "" {
		log.Printf("warning: GRANULAR_GITHUB_TOKEN is empty; the git proxy cannot authenticate to GitHub until it is set")
	}
	return http.ListenAndServe(addr, srv.Handler())
}

// envOr returns the value of the named environment variable, or fallback when it
// is unset or empty.
//
// @arg key The environment variable name.
// @arg fallback The value to return when the variable is unset or empty.
// @return string The variable's value, or fallback.
//
// @testcase TestEnvOrFallback checks the fallback is used for an unset variable.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseDurationOr reads a duration from the named environment variable, returning
// fallback when the variable is unset, empty, or not a valid Go duration.
//
// @arg key The environment variable name.
// @arg fallback The duration to return when the variable is unset or invalid.
// @return time.Duration The parsed duration, or fallback.
//
// @testcase TestParseDurationOr checks parsing, fallback, and invalid input.
func parseDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}
