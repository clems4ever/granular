// Command granular-auth-server is the granular authorization server (AS): the generic
// policy authority. It registers Gateway HMAC credentials, accepts gateway-signed
// grant-request bundles (proposals) from clients, serves the human consent screen
// (GitHub login, gated on the approver email), and verifies operations against the
// policy attached to a token. It holds no platform credentials and understands no
// permission vocabulary.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	asconfig "github.com/clems4ever/granular/auth_server/config"
	"github.com/clems4ever/granular/auth_server/server"
	"github.com/clems4ever/granular/auth_server/store"
)

// main parses flags, loads the YAML configuration, and starts the authorization
// server.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only delegates to run.
func main() {
	configPath := flag.String("config", "granular-auth.yaml", "path to the YAML configuration file")
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
// file does not exist, so the server runs out of the box without one.
//
// @arg path The path to the YAML configuration file.
// @return *asconfig.Config The loaded or default configuration.
// @error error when the file exists but cannot be read or parsed.
//
// @testcase TestLoadConfigMissingUsesDefaults falls back to defaults for a missing file.
func loadConfig(path string) (*asconfig.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		log.Printf("config file %q not found; using built-in defaults", path)
		return asconfig.Default(), nil
	}
	cfg, err := asconfig.Load(path)
	if err != nil {
		return nil, err
	}
	log.Printf("loaded configuration from %q", path)
	return cfg, nil
}

// run builds the authorization server from cfg and serves until the process is
// stopped.
//
// @arg cfg The authorization-server configuration.
// @error error Any error from configuration or from ListenAndServe.
//
// @testcase TestRunRejectsBadWorkspace checks run fails when the workspace cannot be created.
func run(cfg *asconfig.Config) error {
	if err := os.MkdirAll(cfg.Workspace, 0o755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	gateways := cfg.GatewaySecrets()
	srv := server.New(st, cfg.BaseURL, gateways)
	srv.UseAdminToken(cfg.AdminToken)

	auth := server.NewAuthenticator(server.AuthConfig{
		ClientID:      cfg.Auth.ClientID,
		ClientSecret:  cfg.Auth.ClientSecret,
		SessionSecret: []byte(cfg.Auth.SessionSecret),
		BaseURL:       cfg.BaseURL,
	})
	srv.UseAuth(auth)

	if len(gateways) == 0 {
		log.Printf("warning: no gateways registered; /api/proposals and /api/verify will reject every caller until a gateway is configured")
	} else {
		log.Printf("%d gateway(s) registered", len(gateways))
	}
	if cfg.AdminToken == "" {
		log.Printf("warning: no admin_token_file configured; policy administration (PUT/GET/DELETE /api/policy) is disabled until one is set")
	}
	if !auth.Enabled() {
		log.Printf("warning: consent UI is UNAUTHENTICATED; set auth.client_id and auth.client_secret_file to require a GitHub login (OAuth app callback URL: %s%s)", cfg.BaseURL, callbackURLSuffix)
	} else {
		log.Printf("consent UI authentication enabled (GitHub OAuth); approval is gated on each proposal's approver email")
	}

	stop := make(chan struct{})
	defer close(stop)
	st.StartCleanup(stop, cfg.CleanupInterval.Std(), func(n int) {
		log.Printf("cleaned up %d expired grant(s)", n)
	})
	log.Printf("grant janitor purging expired grants every %s", cfg.CleanupInterval.Std())

	log.Printf("granular-auth-server listening on %s (base URL %s, workspace %s)", cfg.Addr, cfg.BaseURL, cfg.Workspace)
	return http.ListenAndServe(cfg.Addr, srv.Handler())
}

// callbackURLSuffix is the GitHub OAuth callback path, surfaced in the startup
// warning so an operator knows how to register the OAuth app.
const callbackURLSuffix = "/auth/github/callback"
