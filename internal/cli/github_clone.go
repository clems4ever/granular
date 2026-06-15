package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// newCloneCmd builds "github clone <repo> <dest>", which clones a repository
// locally through the server's git proxy after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The clone command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newCloneCmd(server *string) *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "clone <repo> <dest>",
		Short: "Clone a GitHub repository locally through the granular proxy",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.Operation{
				Type:   "github.clone",
				Params: map[string]any{"repo": args[0]},
			}
			return runClone(cmd.Context(), client.New(*server), req, args[1], ref, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "branch or tag to check out (default: repository default branch)")
	return cmd
}

// runClone requests authorization to clone, then performs the clone locally
// against the server's git proxy.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The github.clone operation request.
// @arg dest The local destination directory for the clone.
// @arg ref An optional branch or tag to check out.
// @arg out The writer for user-facing output.
// @error error when authorization fails or the local git clone fails.
//
// @testcase TestRunClonePendingPrintsURL prints the approval URL when pending.
// @testcase TestRunCloneClonesViaProxy clones from the brokered URL once authorized.
func runClone(ctx context.Context, c *client.Client, req api.Operation, dest, ref string, out io.Writer) error {
	resp, done, err := authorize(ctx, c, req, "perform the clone", out)
	if err != nil || done {
		return err
	}

	cloneURL, _ := resp.Result["clone_url"].(string)
	if cloneURL == "" {
		return fmt.Errorf("server did not return a clone URL")
	}
	fmt.Fprintf(out, "Authorized. Cloning %v into %s via the granular proxy...\n", resp.Result["repo"], dest)
	return gitClone(ctx, cloneURL, dest, ref, out)
}

// gitClone runs the local `git clone` against the brokered proxy URL.
//
// @arg ctx Context for cancellation of the git process.
// @arg cloneURL The brokered proxy URL to clone from.
// @arg dest The local destination directory.
// @arg ref An optional branch or tag; when set, a single-branch checkout is used.
// @arg out The writer that receives git's stdout and stderr.
// @error error when git is unavailable or exits non-zero.
//
// @testcase TestRunCloneClonesViaProxy drives a successful clone through this helper.
func gitClone(ctx context.Context, cloneURL, dest, ref string, out io.Writer) error {
	args := []string{"clone"}
	if ref != "" {
		args = append(args, "--branch", ref, "--single-branch")
	}
	args = append(args, cloneURL, dest)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}
	fmt.Fprintln(out, "Clone completed.")
	return nil
}
