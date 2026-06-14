// Command granular is the CLI client. The command tree lives in internal/cli;
// this binary is a thin entrypoint.
package main

import (
	"fmt"
	"os"

	"github.com/clems4ever/granular/internal/cli"
)

// main builds the command tree and executes it, exiting non-zero on error.
func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
