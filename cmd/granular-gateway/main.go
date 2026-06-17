// Command granular-gateway is the granular GitHub gateway (Resource Server): it holds
// the GitHub credential and the permission vocabulary. It serves the permission schema,
// signs grant requests for clients, and executes operations only after the authorization
// server (AS) confirms they are authorized. The gateway logic is the generic gateway
// SDK; this command wires the GitHub implementation (gateway-github) into it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/clems4ever/granular/gateway"
	gatewaygithub "github.com/clems4ever/granular/gateway-github"
	gwconfig "github.com/clems4ever/granular/gateway-github/config"
	"github.com/clems4ever/granular/gateway/asclient"
	"github.com/clems4ever/granular/internal/operations"
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

// run builds the GitHub gateway from cfg and serves until the process is stopped.
//
// @arg cfg The gateway configuration.
// @error error Any error from ListenAndServe.
//
// @testcase TestRunRejectsBadConfig is a placeholder for run wiring.
func run(cfg *gwconfig.Config) error {
	env := operations.Env{GitHubToken: cfg.GitHubToken, BaseURL: cfg.BaseURL}

	if cfg.Secret == "" {
		log.Printf("warning: secret is empty; the AS will reject this gateway's signatures and verify calls until secret_file is set")
	}
	if cfg.GitHubToken == "" {
		log.Printf("warning: github_token is empty; github.* operations cannot authenticate until it is set")
	}

	verifier := asclient.New(cfg.ASURL, cfg.GatewayID, []byte(cfg.Secret))
	gw := gateway.New(gateway.Config{
		Schema:    gatewaygithub.Schema(),
		Registry:  gatewaygithub.Registry(env),
		GatewayID: cfg.GatewayID,
		Secret:    []byte(cfg.Secret),
		Verifier:  verifier,
	})

	log.Printf("granular-gateway %q listening on %s (base URL %s, AS %s)", cfg.GatewayID, cfg.Addr, cfg.BaseURL, cfg.ASURL)
	return fmt.Errorf("server stopped: %w", http.ListenAndServe(cfg.Addr, gw.Handler()))
}
