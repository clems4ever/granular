// Package cli builds the granular CLI command tree. The cmd/granular binary is a
// thin main that delegates here; each sub-command represents one granular
// operation and walks the user through approval when it is not yet granted.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// NewRootCmd builds the root "granular" command with shared flags and its
// sub-command tree.
//
// @return *cobra.Command The configured root command.
//
// @testcase TestRootCommandTree checks the github sub-commands are wired.
func NewRootCmd() *cobra.Command {
	var server string
	root := &cobra.Command{
		Use:           "granular",
		Short:         "Request granular, human-approved operations on third-party platforms",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&server, "server", "http://localhost:8080", "granular server base URL")

	root.AddCommand(
		newGithubCmd(&server),
		newRequestCmd(&server),
		newCatalogCmd(&server),
	)
	return root
}

// authorize submits an operation. When approval is still pending it prints the
// approval URL and returns done=true; when the operation is authorised it returns
// the response with done=false so the caller can fulfil it.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The operation to attempt.
// @arg action A short verb phrase for the "re-run to <action>" hint, e.g. "clone".
// @arg out The writer for user-facing output.
// @return api.OperationResponse The server response (meaningful when done is false).
// @return bool True when approval is pending and the URL was printed.
// @error error when the call fails or the server returns an unexpected status.
//
// @testcase TestRunClonePendingPrintsURL covers the pending branch.
// @testcase TestRunIssueListPrintsIssues covers the authorised branch.
func authorize(ctx context.Context, c *client.Client, req api.OperationRequest, action string, out io.Writer) (api.OperationResponse, bool, error) {
	resp, err := c.Submit(ctx, req)
	if err != nil {
		return resp, false, err
	}
	switch resp.Status {
	case api.StatusPending:
		fmt.Fprintf(out, "Approval required. Open this URL to approve or deny:\n\n  %s\n\nOnce approved, re-run the same command to %s.\n", resp.ApprovalURL, action)
		return resp, true, nil
	case api.StatusCompleted:
		return resp, false, nil
	default:
		return resp, false, fmt.Errorf("operation not authorized: status=%s %s", resp.Status, resp.Error)
	}
}
