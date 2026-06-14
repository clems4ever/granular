package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// newIssueCommentCmd builds "github issue comment <repo> <number>", which posts a
// comment on an issue after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The issue comment command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newIssueCommentCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		body     string
		bodyFile string
	)
	cmd := &cobra.Command{
		Use:   "comment <repo> <number>",
		Short: "Post a comment on a GitHub issue",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid issue number %q", args[1])
			}
			text, err := resolveBody(body, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if text == "" {
				return fmt.Errorf("a comment body is required (use --body or --body-file)")
			}
			req := api.OperationRequest{
				Type: "github.issue.comment",
				Params: map[string]any{
					"repo":   args[0],
					"number": number,
					"body":   text,
				},
			}
			return runIssueComment(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVarP(&body, "body", "b", "", "the comment body")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "read the comment body from a file (\"-\" for stdin)")
	return cmd
}

// newIssueCreateCmd builds "github issue create <repo>", which creates a new issue
// after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The issue create command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newIssueCreateCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		title     string
		body      string
		bodyFile  string
		labels    []string
		assignees []string
	)
	cmd := &cobra.Command{
		Use:   "create <repo>",
		Short: "Create a new GitHub issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := resolveBody(body, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			req := api.OperationRequest{
				Type: "github.issue.create",
				Params: map[string]any{
					"repo":      args[0],
					"title":     title,
					"body":      text,
					"labels":    labels,
					"assignees": assignees,
				},
			}
			return runIssueCreate(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVarP(&title, "title", "t", "", "the issue title (required)")
	cmd.Flags().StringVarP(&body, "body", "b", "", "the issue body")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "read the issue body from a file (\"-\" for stdin)")
	cmd.Flags().StringArrayVarP(&labels, "label", "l", nil, "label to add (repeatable)")
	cmd.Flags().StringArrayVarP(&assignees, "assignee", "a", nil, "user to assign (repeatable)")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

// runIssueComment requests authorization to post a comment and, once authorised,
// posts it and reports the created comment.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.issue.comment operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw created-comment JSON.
// @error error when authorization or the post fails.
//
// @testcase TestRunIssueCommentPendingPrintsURL prints the approval URL when pending.
// @testcase TestRunIssueCommentReportsResult prints the created comment URL.
func runIssueComment(ctx context.Context, c *client.Client, req api.OperationRequest, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "post the comment", out)
	if err != nil || done {
		return err
	}
	if jsonOut {
		return printJSON(out, resp.Result)
	}
	fmt.Fprintf(out, "Comment posted: %v\n", resp.Result["html_url"])
	return nil
}

// runIssueCreate requests authorization to create an issue and, once authorised,
// creates it and reports the new issue.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.issue.create operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw created-issue JSON.
// @error error when authorization or the creation fails.
//
// @testcase TestRunIssueCreateReportsResult prints the created issue number and URL.
func runIssueCreate(ctx context.Context, c *client.Client, req api.OperationRequest, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "create the issue", out)
	if err != nil || done {
		return err
	}
	if jsonOut {
		return printJSON(out, resp.Result)
	}
	fmt.Fprintf(out, "Issue created: #%v %v\n", asInt(resp.Result["number"]), resp.Result["html_url"])
	return nil
}

// resolveBody returns the body text, reading it from bodyFile when set ("-" reads
// stdin), otherwise returning the inline body.
//
// @arg body The inline body value from --body.
// @arg bodyFile The file path from --body-file, or "" when unset.
// @arg stdin The reader used when bodyFile is "-".
// @return string The resolved body text.
// @error error when the body file cannot be read.
//
// @testcase TestResolveBodyFromFile reads a body from a file.
func resolveBody(body, bodyFile string, stdin io.Reader) (string, error) {
	if bodyFile == "" {
		return body, nil
	}
	if bodyFile == "-" {
		data, err := io.ReadAll(stdin)
		return string(data), err
	}
	data, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
