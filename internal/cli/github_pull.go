package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// newPullCmd builds the "github pr" command grouping pull request operations. Its
// persistent --json flag is inherited by every pr sub-command.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The pr command with its sub-commands.
//
// @testcase TestRootCommandTree verifies this command is attached.
func newPullCmd(server *string) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{Use: "pr", Short: "GitHub pull request operations"}
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output the raw JSON result instead of formatted text")
	cmd.AddCommand(
		newPullListCmd(server, &jsonOut),
		newPullViewCmd(server, &jsonOut),
		newPullDiffCmd(server, &jsonOut),
		newPullCreateCmd(server, &jsonOut),
		newPullCommentCmd(server, &jsonOut),
		newPullReviewCmd(server, &jsonOut),
		newPullEditCmd(server, &jsonOut),
		newPullMergeCmd(server, &jsonOut),
		newPullCloseCmd(server, &jsonOut),
		newPullReopenCmd(server, &jsonOut),
	)
	return cmd
}

// newPullListCmd builds "github pr list <repo>", which lists a repository's pull
// requests after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr list command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullListCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		state string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list <repo>",
		Short: "List pull requests of a GitHub repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.OperationRequest{
				Type:   "github.pull.list",
				Params: map[string]any{"repo": args[0], "state": state, "limit": limit},
			}
			return runPullList(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVar(&state, "state", "open", "filter by state: open, closed or all")
	cmd.Flags().IntVar(&limit, "limit", 30, "maximum number of pull requests to list")
	return cmd
}

// newPullViewCmd builds "github pr view <repo> <number>", which shows a single
// pull request's details after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr view command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullViewCmd(server *string, jsonOut *bool) *cobra.Command {
	var comments bool
	cmd := &cobra.Command{
		Use:   "view <repo> <number>",
		Short: "Show the details of a GitHub pull request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			req := api.OperationRequest{
				Type:   "github.pull.view",
				Params: map[string]any{"repo": args[0], "number": number, "comments": comments},
			}
			return runPullView(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().BoolVar(&comments, "comments", false, "include the conversation (comments and reviews, approved as a separate grant)")
	return cmd
}

// newPullDiffCmd builds "github pr diff <repo> <number>", which shows a pull
// request's unified diff after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr diff command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullDiffCmd(server *string, jsonOut *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <repo> <number>",
		Short: "Show the unified diff of a GitHub pull request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			req := api.OperationRequest{
				Type:   "github.pull.diff",
				Params: map[string]any{"repo": args[0], "number": number},
			}
			return runPullDiff(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	return cmd
}

// runPullList requests authorization to list pull requests and, once authorised,
// prints the pull requests returned by the server.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.pull.list operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw pulls JSON instead of formatted text.
// @error error when authorization or the listing fails.
//
// @testcase TestRunPullListPrintsPulls prints the pull requests once authorized.
// @testcase TestRunPullListJSON prints the pulls as JSON when jsonOut is set.
func runPullList(ctx context.Context, c *client.Client, req api.OperationRequest, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "list the pull requests", out)
	if err != nil || done {
		return err
	}
	pulls, _ := resp.Result["pulls"].([]any)
	if jsonOut {
		return printJSON(out, pulls)
	}
	printPulls(out, pulls)
	return nil
}

// printPulls renders the pull request list returned by the server.
//
// @arg out The writer for user-facing output.
// @arg pulls The raw GitHub pull request objects (each a decoded JSON map).
//
// @testcase TestRunPullListPrintsPulls checks a pull request line is rendered.
func printPulls(out io.Writer, pulls []any) {
	if len(pulls) == 0 {
		fmt.Fprintln(out, "No pull requests found.")
		return
	}
	for _, raw := range pulls {
		pull, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(out, "#%-5v %-6v %s  (%v)\n",
			asInt(pull["number"]), pull["state"], pull["title"], userLogin(pull))
	}
}

// runPullView requests authorization to view a pull request and, once authorised,
// prints its details.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.pull.view operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw pull request JSON instead of formatted text.
// @error error when authorization or the lookup fails.
//
// @testcase TestRunPullViewPrintsPull prints the pull request details once authorized.
func runPullView(ctx context.Context, c *client.Client, req api.OperationRequest, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "view the pull request", out)
	if err != nil || done {
		return err
	}
	if jsonOut {
		return printJSON(out, resp.Result)
	}
	printPull(out, resp.Result)
	return nil
}

// printPull renders a single pull request's details returned by the server.
//
// @arg out The writer for user-facing output.
// @arg pull The decoded pull request detail map.
//
// @testcase TestRunPullViewPrintsPull checks the title, branches and body are rendered.
func printPull(out io.Writer, pull map[string]any) {
	fmt.Fprintf(out, "#%v  %v\n", asInt(pull["number"]), pull["title"])
	fmt.Fprintf(out, "State:    %v\n", pull["state"])
	fmt.Fprintf(out, "Author:   %v\n", userLogin(pull))
	fmt.Fprintf(out, "Branches: %s -> %s\n", refName(pull, "head"), refName(pull, "base"))
	fmt.Fprintf(out, "URL:      %v\n", pull["html_url"])
	if body, _ := pull["body"].(string); body != "" {
		fmt.Fprintf(out, "\n%s\n", body)
	}
	if comments, ok := pull["comments_list"].([]any); ok {
		printComments(out, comments)
	}
}

// refName extracts the "ref" of a pull request's "head" or "base" object.
//
// @arg pull The raw GitHub pull request object.
// @arg side The side to read, "head" or "base".
// @return string The branch ref, or "" when absent.
//
// @testcase TestRunPullViewPrintsPull checks the head/base refs are rendered.
func refName(pull map[string]any, side string) string {
	obj, _ := pull[side].(map[string]any)
	ref, _ := obj["ref"].(string)
	return ref
}

// runPullDiff requests authorization to view a pull request's diff and, once
// authorised, prints the diff returned by the server.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.pull.diff operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw result JSON instead of the diff text.
// @error error when authorization or the lookup fails.
//
// @testcase TestRunPullDiffPrintsDiff prints the diff once authorized.
func runPullDiff(ctx context.Context, c *client.Client, req api.OperationRequest, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "view the pull request diff", out)
	if err != nil || done {
		return err
	}
	if jsonOut {
		return printJSON(out, resp.Result)
	}
	diff, _ := resp.Result["diff"].(string)
	fmt.Fprint(out, diff)
	if diff != "" && diff[len(diff)-1] != '\n' {
		fmt.Fprintln(out)
	}
	return nil
}
