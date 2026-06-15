package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// newIssueCmd builds the "github issue" command grouping issue operations. Its
// persistent --json flag is inherited by every issue sub-command.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The issue command with its sub-commands.
//
// @testcase TestRootCommandTree verifies this command is attached.
func newIssueCmd(server *string) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{Use: "issue", Short: "GitHub issue operations"}
	cmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output the raw JSON result instead of formatted text")
	cmd.AddCommand(
		newIssueListCmd(server, &jsonOut),
		newIssueViewCmd(server, &jsonOut),
		newIssueCommentCmd(server, &jsonOut),
		newIssueCreateCmd(server, &jsonOut),
		newIssueEditCmd(server, &jsonOut),
		newIssueCloseCmd(server, &jsonOut),
		newIssueReopenCmd(server, &jsonOut),
	)
	return cmd
}

// newIssueViewCmd builds "github issue view <repo> <number>", which shows a single
// issue's details after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The issue view command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newIssueViewCmd(server *string, jsonOut *bool) *cobra.Command {
	var comments bool
	cmd := &cobra.Command{
		Use:   "view <repo> <number>",
		Short: "Show the details of a GitHub issue",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid issue number %q", args[1])
			}
			req := api.Operation{
				Type: "github.issue.view",
				Params: map[string]any{
					"repo":     args[0],
					"number":   number,
					"comments": comments,
				},
			}
			return runIssueView(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().BoolVar(&comments, "comments", false, "include the issue's comments (approved as a separate grant)")
	return cmd
}

// newIssueListCmd builds "github issue list <repo>", which lists a repository's
// issues after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The issue list command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newIssueListCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		state string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list <repo>",
		Short: "List issues of a GitHub repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.Operation{
				Type: "github.issue.list",
				Params: map[string]any{
					"repo":  args[0],
					"state": state,
					"limit": limit,
				},
			}
			return runIssueList(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
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
// @arg jsonOut When true, print the raw issues JSON instead of formatted text.
// @error error when authorization or the listing fails.
//
// @testcase TestRunIssueListPendingPrintsURL prints the approval URL when pending.
// @testcase TestRunIssueListPrintsIssues prints the issues once authorized.
// @testcase TestRunIssueListJSON prints the issues as JSON when jsonOut is set.
func runIssueList(ctx context.Context, c *client.Client, req api.Operation, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "list the issues", out)
	if err != nil || done {
		return err
	}
	issues, _ := resp.Result["issues"].([]any)
	if jsonOut {
		return printJSON(out, issues)
	}
	printIssues(out, issues)
	return nil
}

// printIssues renders the issue list returned by the server.
//
// @arg out The writer for user-facing output.
// @arg issues The raw GitHub issue objects (each a decoded JSON map).
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
			asInt(issue["number"]), issue["state"], issue["title"], userLogin(issue))
	}
}

// runIssueView requests authorization to view an issue and, once authorised,
// prints the issue details returned by the server.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.issue.view operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw issue JSON instead of formatted text.
// @error error when authorization or the lookup fails.
//
// @testcase TestRunIssueViewPendingPrintsURL prints the approval URL when pending.
// @testcase TestRunIssueViewPrintsIssue prints the issue details once authorized.
// @testcase TestRunIssueViewJSON prints the issue as JSON when jsonOut is set.
func runIssueView(ctx context.Context, c *client.Client, req api.Operation, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "view the issue", out)
	if err != nil || done {
		return err
	}
	if jsonOut {
		return printJSON(out, resp.Result)
	}
	printIssue(out, resp.Result)
	return nil
}

// printIssue renders a single issue's details returned by the server.
//
// @arg out The writer for user-facing output.
// @arg issue The decoded issue detail map.
//
// @testcase TestRunIssueViewPrintsIssue checks the title and body are rendered.
func printIssue(out io.Writer, issue map[string]any) {
	fmt.Fprintf(out, "#%v  %v\n", asInt(issue["number"]), issue["title"])
	fmt.Fprintf(out, "State:    %v\n", issue["state"])
	fmt.Fprintf(out, "Author:   %v\n", userLogin(issue))
	if labels := labelNames(issue); labels != "" {
		fmt.Fprintf(out, "Labels:   %s\n", labels)
	}
	fmt.Fprintf(out, "Comments: %v\n", asInt(issue["comments"]))
	fmt.Fprintf(out, "URL:      %v\n", issue["html_url"])
	if body, _ := issue["body"].(string); body != "" {
		fmt.Fprintf(out, "\n%s\n", body)
	}
	if comments, ok := issue["comments_list"].([]any); ok {
		printComments(out, comments)
	}
}

// printComments renders the comments fetched alongside an issue (when --comments
// was requested).
//
// @arg out The writer for user-facing output.
// @arg comments The raw GitHub comment objects.
//
// @testcase TestRunIssueViewPrintsComments checks a comment body is rendered.
func printComments(out io.Writer, comments []any) {
	fmt.Fprintf(out, "\n--- %d comment(s) ---\n", len(comments))
	for _, raw := range comments {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(out, "\n%v wrote:\n%v\n", userLogin(c), c["body"])
	}
}

// userLogin extracts "user.login" from a decoded GitHub issue object.
//
// @arg issue The raw GitHub issue object.
// @return string The author's login, or "" when absent.
//
// @testcase TestRunIssueListPrintsIssues checks the author is rendered.
func userLogin(issue map[string]any) string {
	user, _ := issue["user"].(map[string]any)
	login, _ := user["login"].(string)
	return login
}

// labelNames joins the "name" of each entry in a GitHub issue's "labels" array.
//
// @arg issue The raw GitHub issue object.
// @return string A comma-separated list of label names, or "" when none.
//
// @testcase TestRunIssueViewPrintsIssue checks labels are rendered.
func labelNames(issue map[string]any) string {
	labels, _ := issue["labels"].([]any)
	names := make([]string, 0, len(labels))
	for _, l := range labels {
		if m, ok := l.(map[string]any); ok {
			if n, ok := m["name"].(string); ok {
				names = append(names, n)
			}
		}
	}
	return strings.Join(names, ", ")
}

// printJSON writes v as indented JSON followed by a newline.
//
// @arg out The writer for user-facing output.
// @arg v The value to encode (the issues slice or the issue detail map).
// @error error when v cannot be marshalled.
//
// @testcase TestRunIssueViewJSON checks valid JSON is emitted.
func printJSON(out io.Writer, v any) error {
	encoded, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(encoded))
	return err
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
