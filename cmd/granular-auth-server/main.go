// Command granular-auth-server is the granular authorization server (AS): the generic
// policy authority. It registers ResourceServer HMAC credentials, accepts resource server-signed
// grant-request bundles (proposals) from clients, serves the human consent screen
// (GitHub login, gated on the approver email), and verifies operations against the
// subject identified by a token. It holds no platform credentials and understands no
// permission vocabulary. The command tree lives in package auth_server/cli; this
// binary is a thin entrypoint that builds and executes it.
package main

import (
	"fmt"
	"os"

	authcli "github.com/clems4ever/granular/auth_server/cli"
)

// main builds the command tree and executes it, exiting non-zero on error.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only builds and executes the tree.
func main() {
	if err := authcli.NewRootCmd(os.Stdout).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
