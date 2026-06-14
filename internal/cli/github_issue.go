package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// newIssueCmd builds the "github issue" command grouping issue operations.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The issue command with its sub-commands.
//
// @testcase TestRootCommandTree verifies this command is attached.
func newIssueCmd(server *string) *cobra.Command {
	cmd := &cobra.Command{Use: "issue", Short: "GitHub issue operations"}
	cmd.AddCommand(newIssueListCmd(server))
	return cmd
}

// newIssueListCmd builds "github issue list <repo>", which lists a repository's
// issues after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The issue list command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newIssueListCmd(server *string) *cobra.Command {
	var (
		state string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list <repo>",
		Short: "List issues of a GitHub repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.OperationRequest{
				Type: "github.issue.list",
				Params: map[string]any{
					"repo":  args[0],
					"state": state,
					"limit": limit,
				},
			}
			return runIssueList(cmd.Context(), client.New(*server), req, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&state, "state", "open", "filter by state: open, closed or all")
	cmd.Flags().IntVar(&limit, "limit", 30, "maximum number of issues to list")
	return cmd
}

// runIssueList requests authorization to list issues and, once authorised, prints
// the issues returned by the server.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.issue.list operation request.
// @arg out The writer for user-facing output.
// @error error when authorization or the listing fails.
//
// @testcase TestRunIssueListPendingPrintsURL prints the approval URL when pending.
// @testcase TestRunIssueListPrintsIssues prints the issues once authorized.
func runIssueList(ctx context.Context, c *client.Client, req api.OperationRequest, out io.Writer) error {
	resp, done, err := authorize(ctx, c, req, "list the issues", out)
	if err != nil || done {
		return err
	}
	issues, _ := resp.Result["issues"].([]any)
	printIssues(out, issues)
	return nil
}

// printIssues renders the issue list returned by the server.
//
// @arg out The writer for user-facing output.
// @arg issues The decoded issues, each a map with number/title/state/author.
//
// @testcase TestRunIssueListPrintsIssues checks an issue line is rendered.
func printIssues(out io.Writer, issues []any) {
	if len(issues) == 0 {
		fmt.Fprintln(out, "No issues found.")
		return
	}
	for _, raw := range issues {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(out, "#%-5v %-6v %s  (%v)\n",
			asInt(issue["number"]), issue["state"], issue["title"], issue["author"])
	}
}

// asInt renders a JSON-decoded number (float64) as an int for display.
//
// @arg v A value that may be a float64, int or other type.
// @return int The integer value, or 0 when v is not numeric.
//
// @testcase TestRunIssueListPrintsIssues exercises number formatting.
func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
