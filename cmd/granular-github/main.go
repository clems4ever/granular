// Command granular-github is the granular GitHub resource-server CLI. The command
// tree lives in package githubcli; this binary is a thin entrypoint that builds
// and executes it.
package main

import (
	"fmt"
	"os"

	githubcli "github.com/clems4ever/granular/resourceserver-github/cli"
)

// main builds the command tree and executes it, exiting non-zero on error.
//
// @testcase TestNewRootCmdHasGitHubCommands covers the command tree main executes.
func main() {
	if err := githubcli.NewRootCmd(os.Stdout).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
