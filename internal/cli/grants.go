package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/client"
)

// newGrantsCmd builds the "grants" command grouping grant listing and revocation.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The grants command with its sub-commands.
//
// @testcase TestRootCommandTree verifies this command is attached.
func newGrantsCmd(server *string) *cobra.Command {
	cmd := &cobra.Command{Use: "grants", Short: "List and revoke active grants"}
	cmd.AddCommand(newGrantsListCmd(server), newGrantsRevokeCmd(server))
	return cmd
}

// newGrantsListCmd builds "grants list", which shows the active grants and the
// request history.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The grants list command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newGrantsListCmd(server *string) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active grants and the request history",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGrantsList(cmd.Context(), client.New(*server), cmd.OutOrStdout(), jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "output the raw JSON instead of formatted text")
	return cmd
}

// newGrantsRevokeCmd builds "grants revoke <id>", which revokes the active grants
// for a grant id or a request id.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The grants revoke command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newGrantsRevokeCmd(server *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke active grants by grant id or request id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGrantsRevoke(cmd.Context(), client.New(*server), args[0], cmd.OutOrStdout())
		},
	}
	return cmd
}

// runGrantsList fetches and prints the active grants and request history.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw JSON instead of formatted text.
// @error error when the request fails.
//
// @testcase TestRunGrantsListPrints prints active grants in text form.
// @testcase TestRunGrantsListJSON prints the grants as JSON when jsonOut is set.
func runGrantsList(ctx context.Context, c *client.Client, out io.Writer, jsonOut bool) error {
	resp, err := c.Grants(ctx)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(out, resp)
	}
	fmt.Fprintf(out, "Active grants (%d):\n", len(resp.Grants))
	if len(resp.Grants) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	for _, g := range resp.Grants {
		fmt.Fprintf(out, "  %s  %-22s expires %s\n    %s\n", g.ID, g.OperationType, g.ExpiresAt, firstLineCLI(g.Description))
	}
	fmt.Fprintf(out, "\nRequest history (%d):\n", len(resp.Requests))
	for _, r := range resp.Requests {
		fmt.Fprintf(out, "  %s  %-22s %s\n", r.ID, r.OperationType, r.Status)
	}
	return nil
}

// runGrantsRevoke revokes grants for the given id and reports the count.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg id The grant id or request id to revoke.
// @arg out The writer for user-facing output.
// @error error when the revoke call fails or nothing matched.
//
// @testcase TestRunGrantsRevokeReportsCount prints how many grants were revoked.
func runGrantsRevoke(ctx context.Context, c *client.Client, id string, out io.Writer) error {
	resp, err := c.Revoke(ctx, id)
	if err != nil {
		return err
	}
	if resp.Revoked > 0 {
		fmt.Fprintf(out, "Revoked %d active grant(s) for %s.\n", resp.Revoked, id)
	} else {
		fmt.Fprintf(out, "Revoked %s (no active grants remained).\n", id)
	}
	return nil
}

// firstLineCLI returns the first line of s for compact one-line display.
//
// @arg s The (possibly multi-line) string.
// @return string The first line.
//
// @testcase TestRunGrantsListPrints relies on single-line grant descriptions.
func firstLineCLI(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
