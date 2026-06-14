// Command granular is the CLI client. Each sub-command represents one granular
// operation; the CLI asks the server to perform it and walks the user through
// approval when the operation is not yet granted.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// main builds the command tree and executes it, exiting non-zero on error.
func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// newRootCmd builds the root "granular" command with shared flags and its
// sub-command tree.
//
// @return *cobra.Command The configured root command.
//
// @testcase TestRootCommandHasGithubChild checks the github sub-command is wired.
func newRootCmd() *cobra.Command {
	var server string
	root := &cobra.Command{
		Use:           "granular",
		Short:         "Request granular, human-approved operations on third-party platforms",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&server, "server", "http://localhost:8080", "granular server base URL")

	root.AddCommand(newGithubCmd(&server))
	return root
}

// newGithubCmd builds the "github" command grouping GitHub operations.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The github command with its sub-commands.
//
// @testcase TestRootCommandHasGithubChild verifies this command is attached.
func newGithubCmd(server *string) *cobra.Command {
	cmd := &cobra.Command{Use: "github", Short: "GitHub operations"}
	cmd.AddCommand(newCloneCmd(server))
	return cmd
}

// newCloneCmd builds "github clone <repo> <dest>", which clones a repository on
// the server after approval.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The clone command.
//
// @testcase TestRootCommandHasGithubChild reaches this command through the tree.
func newCloneCmd(server *string) *cobra.Command {
	var ref string
	cmd := &cobra.Command{
		Use:   "clone <repo> <dest>",
		Short: "Clone a GitHub repository locally through the granular proxy",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := api.OperationRequest{
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
// against the server's git proxy. If approval is required it prints the approval
// URL and returns; the user approves out-of-band and re-runs.
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
func runClone(ctx context.Context, c *client.Client, req api.OperationRequest, dest, ref string, out io.Writer) error {
	resp, err := c.Submit(ctx, req)
	if err != nil {
		return err
	}

	switch resp.Status {
	case api.StatusPending:
		fmt.Fprintf(out, "Approval required. Open this URL to approve or deny:\n\n  %s\n\nOnce approved, re-run the same command to perform the clone.\n", resp.ApprovalURL)
		return nil
	case api.StatusCompleted:
		cloneURL, _ := resp.Result["clone_url"].(string)
		if cloneURL == "" {
			return fmt.Errorf("server did not return a clone URL")
		}
		fmt.Fprintf(out, "Authorized. Cloning %v into %s via the granular proxy...\n", resp.Result["repo"], dest)
		return gitClone(ctx, cloneURL, dest, ref, out)
	default:
		return fmt.Errorf("clone not authorized: status=%s %s", resp.Status, resp.Error)
	}
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
