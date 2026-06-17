// Command granular-client is the granular CLI client. The command tree lives in package
// clientcli; this binary is a thin entrypoint that builds and executes it.
package main

import (
	"fmt"
	"os"

	"github.com/clems4ever/granular/clientcli"
)

// main builds the command tree and executes it, exiting non-zero on error.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only builds and executes the tree.
func main() {
	if err := clientcli.NewRootCmd(os.Stdout).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
