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

// newPullCreateCmd builds "github pr create <repo>", which opens a new pull
// request after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr create command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullCreateCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		title    string
		body     string
		bodyFile string
		head     string
		base     string
		draft    bool
	)
	cmd := &cobra.Command{
		Use:   "create <repo>",
		Short: "Open a new GitHub pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := resolveBody(body, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			req := api.Operation{
				Type: "github.pull.create",
				Params: map[string]any{
					"repo": args[0], "title": title, "body": text,
					"head": head, "base": base, "draft": draft,
				},
			}
			return runPullCreate(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVarP(&title, "title", "t", "", "the pull request title (required)")
	cmd.Flags().StringVarP(&body, "body", "b", "", "the pull request body")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "read the body from a file (\"-\" for stdin)")
	cmd.Flags().StringVarP(&head, "head", "H", "", "the branch containing the changes (required)")
	cmd.Flags().StringVarP(&base, "base", "B", "", "the branch to merge into (required)")
	cmd.Flags().BoolVar(&draft, "draft", false, "open the pull request as a draft")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("head")
	_ = cmd.MarkFlagRequired("base")
	return cmd
}

// newPullCommentCmd builds "github pr comment <repo> <number>", which posts a
// comment on a pull request after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr comment command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullCommentCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		body     string
		bodyFile string
	)
	cmd := &cobra.Command{
		Use:   "comment <repo> <number>",
		Short: "Post a comment on a GitHub pull request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			text, err := resolveBody(body, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if text == "" {
				return fmt.Errorf("a comment body is required (use --body or --body-file)")
			}
			req := api.Operation{
				Type:   "github.pull.comment",
				Params: map[string]any{"repo": args[0], "number": number, "body": text},
			}
			return runPullComment(cmd.Context(), client.New(*server), req, cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVarP(&body, "body", "b", "", "the comment body")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "read the comment body from a file (\"-\" for stdin)")
	return cmd
}

// newPullReviewCmd builds "github pr review <repo> <number>", which submits a
// review on a pull request after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr review command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullReviewCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		event    string
		body     string
		bodyFile string
	)
	cmd := &cobra.Command{
		Use:   "review <repo> <number>",
		Short: "Approve, request changes on, or comment-review a pull request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			text, err := resolveBody(body, bodyFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			req := api.Operation{
				Type:   "github.pull.review",
				Params: map[string]any{"repo": args[0], "number": number, "event": event, "body": text},
			}
			return runPullAction(cmd.Context(), client.New(*server), req, "submit the review", "reviewed", cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVarP(&event, "event", "e", "", "review verdict: approve, request_changes or comment (required)")
	cmd.Flags().StringVarP(&body, "body", "b", "", "the review comment")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "read the review comment from a file (\"-\" for stdin)")
	_ = cmd.MarkFlagRequired("event")
	return cmd
}

// newPullEditCmd builds "github pr edit <repo> <number>", which edits a pull
// request's fields after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr edit command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullEditCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		title    string
		body     string
		bodyFile string
		base     string
	)
	cmd := &cobra.Command{
		Use:   "edit <repo> <number>",
		Short: "Edit a GitHub pull request's fields (not its status)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			params := map[string]any{"repo": args[0], "number": number}
			if cmd.Flags().Changed("title") {
				params["title"] = title
			}
			if cmd.Flags().Changed("body") || cmd.Flags().Changed("body-file") {
				text, err := resolveBody(body, bodyFile, cmd.InOrStdin())
				if err != nil {
					return err
				}
				params["body"] = text
			}
			if cmd.Flags().Changed("base") {
				params["base"] = base
			}
			req := api.Operation{Type: "github.pull.edit", Params: params}
			return runPullAction(cmd.Context(), client.New(*server), req, "edit the pull request", "updated", cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVarP(&title, "title", "t", "", "set the pull request title")
	cmd.Flags().StringVarP(&body, "body", "b", "", "set the pull request body")
	cmd.Flags().StringVarP(&bodyFile, "body-file", "F", "", "read the body from a file (\"-\" for stdin)")
	cmd.Flags().StringVarP(&base, "base", "B", "", "retarget the base branch")
	return cmd
}

// newPullMergeCmd builds "github pr merge <repo> <number>", which merges a pull
// request after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr merge command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullMergeCmd(server *string, jsonOut *bool) *cobra.Command {
	var (
		method string
		sha    string
	)
	cmd := &cobra.Command{
		Use:   "merge <repo> <number>",
		Short: "Merge a GitHub pull request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			req := api.Operation{
				Type:   "github.pull.merge",
				Params: map[string]any{"repo": args[0], "number": number, "method": method, "sha": sha},
			}
			return runPullAction(cmd.Context(), client.New(*server), req, "merge the pull request", "merged", cmd.OutOrStdout(), *jsonOut)
		},
	}
	cmd.Flags().StringVarP(&method, "method", "m", "merge", "merge method: merge, squash or rebase")
	cmd.Flags().StringVar(&sha, "sha", "", "require the pull request head to match this SHA")
	return cmd
}

// newPullCloseCmd builds "github pr close <repo> <number>", which closes a pull
// request after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr close command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullCloseCmd(server *string, jsonOut *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "close <repo> <number>",
		Short: "Close a GitHub pull request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			req := api.Operation{
				Type:   "github.pull.close",
				Params: map[string]any{"repo": args[0], "number": number},
			}
			return runPullAction(cmd.Context(), client.New(*server), req, "close the pull request", "closed", cmd.OutOrStdout(), *jsonOut)
		},
	}
	return cmd
}

// newPullReopenCmd builds "github pr reopen <repo> <number>", which reopens a pull
// request after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @arg jsonOut Pointer to the inherited --json flag value.
// @return *cobra.Command The pr reopen command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPullReopenCmd(server *string, jsonOut *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reopen <repo> <number>",
		Short: "Reopen a GitHub pull request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := strconv.Atoi(args[1])
			if err != nil || number <= 0 {
				return fmt.Errorf("invalid pull request number %q", args[1])
			}
			req := api.Operation{
				Type:   "github.pull.reopen",
				Params: map[string]any{"repo": args[0], "number": number},
			}
			return runPullAction(cmd.Context(), client.New(*server), req, "reopen the pull request", "reopened", cmd.OutOrStdout(), *jsonOut)
		},
	}
	return cmd
}

// runPullAction requests authorization for a mutating pull request action and, once
// authorised, reports the result.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The operation request.
// @arg action The verb phrase for the pending "re-run to <action>" hint.
// @arg past The past-tense verb used in the success line, e.g. "merged".
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw result JSON.
// @error error when authorization or the action fails.
//
// @testcase TestRunPullActionReportsResult prints the pull request number and URL.
func runPullAction(ctx context.Context, c *client.Client, req api.Operation, action, past string, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, action, out)
	if err != nil || done {
		return err
	}
	if jsonOut {
		return printJSON(out, resp.Result)
	}
	if url := resp.Result["html_url"]; url != nil {
		fmt.Fprintf(out, "Pull request #%v %s: %v\n", asInt(resp.Result["number"]), past, url)
	} else {
		fmt.Fprintf(out, "Pull request %s.\n", past)
	}
	return nil
}

// runPullComment requests authorization to post a comment and, once authorised,
// posts it and reports the created comment.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.pull.comment operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw created-comment JSON.
// @error error when authorization or the post fails.
//
// @testcase TestRunPullCommentReportsResult prints the created comment URL.
func runPullComment(ctx context.Context, c *client.Client, req api.Operation, out io.Writer, jsonOut bool) error {
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

// runPullCreate requests authorization to open a pull request and, once authorised,
// opens it and reports the new pull request.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.pull.create operation request.
// @arg out The writer for user-facing output.
// @arg jsonOut When true, print the raw created pull request JSON.
// @error error when authorization or the creation fails.
//
// @testcase TestRunPullCreateReportsResult prints the created pull request number and URL.
func runPullCreate(ctx context.Context, c *client.Client, req api.Operation, out io.Writer, jsonOut bool) error {
	resp, done, err := authorize(ctx, c, req, "open the pull request", out)
	if err != nil || done {
		return err
	}
	if jsonOut {
		return printJSON(out, resp.Result)
	}
	fmt.Fprintf(out, "Pull request opened: #%v %v\n", asInt(resp.Result["number"]), resp.Result["html_url"])
	return nil
}
