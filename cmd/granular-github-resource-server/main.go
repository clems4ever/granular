// Command granular-github-resource-server is the granular GitHub resource server: it
// holds the GitHub credential and the permission vocabulary. It serves the permission
// schema, signs grant requests for clients, and executes operations only after the
// authorization server (AS) confirms they are authorized. The resource server logic is
// the generic resource server SDK; this command wires the GitHub implementation
// (resourceserver-github) into it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/clems4ever/granular/resourceserver"
	resourceservergithub "github.com/clems4ever/granular/resourceserver-github"
	rsconfig "github.com/clems4ever/granular/resourceserver-github/config"
	"github.com/clems4ever/granular/resourceserver/asclient"
)

// main parses flags, loads the YAML configuration, and starts the resource server.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only delegates to run.
func main() {
	configPath := flag.String("config", "granular-github-resource-server.yaml", "path to the YAML configuration file")
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
// @return *rsconfig.Config The loaded or default configuration.
// @error error when the file exists but cannot be read or parsed.
//
// @testcase TestLoadConfigMissingUsesDefaults falls back to defaults for a missing file.
func loadConfig(path string) (*rsconfig.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		log.Printf("config file %q not found; using built-in defaults", path)
		return rsconfig.Default(), nil
	}
	cfg, err := rsconfig.Load(path)
	if err != nil {
		return nil, err
	}
	log.Printf("loaded configuration from %q", path)
	return cfg, nil
}

// run builds the GitHub resource server from cfg and serves until the process is stopped.
//
// @arg cfg The resource server configuration.
// @error error Any error from ListenAndServe.
//
// @testcase TestRunRejectsBadConfig is a placeholder for run wiring.
func run(cfg *rsconfig.Config) error {
	if cfg.Secret == "" {
		log.Printf("warning: secret is empty; the AS will reject this resource server's signatures and verify calls until secret_file is set")
	}
	if cfg.GitHubToken == "" {
		log.Printf("warning: github_token is empty; github.* operations cannot authenticate until it is set")
	}

	verifier := asclient.New(cfg.ASURL, cfg.ResourceServerID, []byte(cfg.Secret))
	rs := resourceserver.New(resourceserver.Config{
		Schema:           resourceservergithub.Schema(),
		Registry:         resourceservergithub.Registry(cfg.GitHubToken),
		ResourceServerID: cfg.ResourceServerID,
		Secret:           []byte(cfg.Secret),
		Verifier:         verifier,
	})

	// The git proxy serves authorized `git clone`/`git push` traffic under /git/, and the
	// REST proxy gates the whole GitHub REST API under /api/github/ — both inject the
	// server-held PAT so agents can use real git and GitHub tooling pointed at granular.
	// Everything else (schema, sign, operations, docs) is the generic SDK handler.
	mux := http.NewServeMux()
	mux.Handle("/git/", resourceservergithub.NewGitProxy(cfg.GitHubToken, rs))
	mux.Handle("/api/github/", resourceservergithub.NewRESTProxy(cfg.GitHubToken, rs))
	mux.Handle("/", rs.Handler())

	log.Printf("granular-github-resource-server %q listening on %s (AS %s)", cfg.ResourceServerID, cfg.Addr, cfg.ASURL)
	return fmt.Errorf("server stopped: %w", http.ListenAndServe(cfg.Addr, mux))
}
