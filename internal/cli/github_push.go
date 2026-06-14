package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// newPushCmd builds "github push <repo> <dir>", which pushes a local repository's
// commits to GitHub through the server's git proxy after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The push command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newPushCmd(server *string) *cobra.Command {
	var branch string
	cmd := &cobra.Command{
		Use:   "push <repo> <dir>",
		Short: "Push a local repository to GitHub through the granular proxy",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.OperationRequest{
				Type:   "github.push",
				Params: map[string]any{"repo": args[0]},
			}
			return runPush(cmd.Context(), client.New(*server), req, args[1], branch, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "branch to push (default: the current branch)")
	return cmd
}

// runPush requests authorization to push, then performs the push locally against
// the server's git proxy.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.push operation request.
// @arg dir The local repository directory to push from.
// @arg branch An optional branch to push; when empty the current branch is used.
// @arg out The writer for user-facing output.
// @error error when authorization fails or the local git push fails.
//
// @testcase TestRunPushPendingPrintsURL prints the approval URL when pending.
// @testcase TestRunPushPushesViaProxy pushes to the brokered URL once authorized.
func runPush(ctx context.Context, c *client.Client, req api.OperationRequest, dir, branch string, out io.Writer) error {
	resp, done, err := authorize(ctx, c, req, "perform the push", out)
	if err != nil || done {
		return err
	}

	pushURL, _ := resp.Result["push_url"].(string)
	if pushURL == "" {
		return fmt.Errorf("server did not return a push URL")
	}
	if branch == "" {
		branch, err = currentBranch(ctx, dir)
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "Authorized. Pushing %s of %v to %v via the granular proxy...\n", branch, resp.Result["repo"], dir)
	return gitPush(ctx, dir, pushURL, branch, out)
}

// currentBranch returns the name of the currently checked-out branch in dir.
//
// @arg ctx Context for cancellation of the git process.
// @arg dir The local repository directory.
// @return string The current branch name.
// @error error when git is unavailable, the directory is not a repo, or HEAD is detached.
//
// @testcase TestRunPushPushesViaProxy relies on resolving the current branch.
func currentBranch(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "symbolic-ref", "--short", "HEAD")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("determine current branch: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// gitPush runs the local `git push` of a branch against the brokered proxy URL.
//
// @arg ctx Context for cancellation of the git process.
// @arg dir The local repository directory to push from.
// @arg pushURL The brokered proxy URL to push to.
// @arg branch The branch to push.
// @arg out The writer that receives git's stdout and stderr.
// @error error when git is unavailable or exits non-zero.
//
// @testcase TestRunPushPushesViaProxy drives a successful push through this helper.
func gitPush(ctx context.Context, dir, pushURL, branch string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "push", pushURL, branch)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git push failed: %w", err)
	}
	fmt.Fprintln(out, "Push completed.")
	return nil
}
