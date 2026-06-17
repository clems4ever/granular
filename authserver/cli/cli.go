// Package cli builds the granular-auth-server command: it loads the YAML
// configuration, wires the store, the consent authenticator and the HTTP server
// together, and serves until the process is stopped. The cmd/granular-auth-server
// binary is a thin entrypoint that builds and executes this command.
package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	asconfig "github.com/clems4ever/granular/authserver/config"
	"github.com/clems4ever/granular/authserver/server"
	"github.com/clems4ever/granular/authserver/store"
)

// callbackURLSuffix is the GitHub OAuth callback path, surfaced in the startup
// warning so an operator knows how to register the OAuth app.
const callbackURLSuffix = "/auth/github/callback"

// NewRootCmd builds the root "granular-auth-server" command: it reads the --config
// path, loads the configuration (falling back to defaults when absent), and runs
// the server.
//
// @arg out The writer cobra prints command output to.
// @return *cobra.Command The configured root command.
//
// @testcase TestCommandTree checks the command exposes a config flag.
func NewRootCmd(out io.Writer) *cobra.Command {
	var configPath string
	root := &cobra.Command{
		Use:           "granular-auth-server",
		Short:         "Run the granular authorization server (policy authority + consent UI)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath)
			if err != nil {
				return err
			}
			return run(cfg)
		},
	}
	root.SetOut(out)
	root.Flags().StringVar(&configPath, "config", "granular-auth.yaml", "path to the YAML configuration file")
	return root
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
// @testcase TestRunRejectsBadDataDir checks run fails when the data directory cannot be created.
func run(cfg *asconfig.Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	resourceServers := cfg.ResourceServerSecrets()
	srv := server.New(st, cfg.BaseURL, resourceServers)
	srv.UseAdminToken(cfg.AdminToken)
	srv.UseRequestTTL(cfg.GrantRequestTTL.Std())

	auth := server.NewAuthenticator(server.AuthConfig{
		ClientID:      cfg.Auth.ClientID,
		ClientSecret:  cfg.Auth.ClientSecret,
		SessionSecret: []byte(cfg.Auth.SessionSecret),
		BaseURL:       cfg.BaseURL,
	})
	srv.UseAuth(auth)

	if len(resourceServers) == 0 {
		log.Printf("warning: no resource servers registered; /api/proposals and /api/verify will reject every caller until a resource server is configured")
	} else {
		log.Printf("%d resource server(s) registered", len(resourceServers))
	}
	if cfg.AdminToken == "" {
		log.Printf("warning: no admin_token_file configured; subject administration (PUT/GET/DELETE /api/subject) is disabled until one is set")
	}
	if !auth.Enabled() {
		log.Printf("warning: consent UI is UNAUTHENTICATED; set auth.client_id and auth.client_secret_file to require a GitHub login (OAuth app callback URL: %s%s)", cfg.BaseURL, callbackURLSuffix)
	} else {
		log.Printf("consent UI authentication enabled (GitHub OAuth); approval is gated on each proposal's approver email")
	}

	stop := make(chan struct{})
	defer close(stop)
	st.StartCleanup(stop, cfg.CleanupInterval.Std(), func(n int) {
		log.Printf("cleaned up %d expired item(s)", n)
	})
	log.Printf("janitor purging expired grants and revoking lapsed requests every %s (grant-request TTL %s)", cfg.CleanupInterval.Std(), cfg.GrantRequestTTL.Std())

	log.Printf("granular-auth-server listening on %s (base URL %s, data dir %s)", cfg.Addr, cfg.BaseURL, cfg.DataDir)
	return http.ListenAndServe(cfg.Addr, srv.Handler())
}
