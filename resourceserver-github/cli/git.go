package githubcli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	githubops "github.com/clems4ever/granular/resourceserver-github/internal/operations/github"
	"github.com/clems4ever/granular/rscli"
	"github.com/spf13/cobra"
)

// credentialHelperArg is an inline git credential helper that answers `get` with the
// subject token read from the GRANULAR_SUBJECT_TOKEN environment variable. Passing the
// token through the environment (not argv) keeps it out of the process list. git consults
// it after the proxy challenges the first, unauthenticated request with 401.
const credentialHelperArg = `credential.helper=!f() { test "$1" = get && echo username=granular && echo "password=$GRANULAR_SUBJECT_TOKEN"; }; f`

// gitRun executes the system git with args and the extra environment entries env,
// streaming output to out. It is a package variable so tests can stub the git invocation.
var gitRun = func(ctx context.Context, args, env []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// gitCommands builds the clone and push commands, which run the real git through the
// resource server's authorizing proxy rather than calling the operations endpoint. It is
// wired as the GitHub CLI's rscli.Spec.Extra.
//
// @arg a The shared App supplying the resolved base URL and subject token.
// @return []*cobra.Command The clone and push commands.
//
// @testcase TestGitCloneCommandRunsGit builds and runs the clone command.
func gitCommands(a *rscli.App) []*cobra.Command {
	clone := &cobra.Command{
		Use:   "clone --repo owner/name [dir]",
		Short: "Clone a repository through the granular proxy (real working copy)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, _ := cmd.Flags().GetString("repo")
			var dir string
			if len(args) == 1 {
				dir = args[0]
			}
			return runProxyGit(cmd.Context(), a, repo, cloneArgs, dir)
		},
	}
	clone.Flags().String("repo", "", "repository, owner/name (required)")
	_ = clone.MarkFlagRequired("repo")

	push := &cobra.Command{
		Use:   "push --repo owner/name [refspec...]",
		Short: "Push to a repository through the granular proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, _ := cmd.Flags().GetString("repo")
			dir, _ := cmd.Flags().GetString("dir")
			return runProxyGit(cmd.Context(), a, repo, pushArgs(dir, args), "")
		},
	}
	push.Flags().String("repo", "", "repository, owner/name (required)")
	push.Flags().String("dir", ".", "local repository directory to push from")
	_ = push.MarkFlagRequired("repo")

	return []*cobra.Command{clone, push}
}

// runProxyGit runs a git subcommand against the resource server's git proxy URL for repo,
// supplying the subject token to git through the environment. argsFor builds the git
// argument list from the proxy URL (and, for clone, the destination directory).
//
// @arg ctx Context for cancellation.
// @arg a The App holding the resolved base URL and token.
// @arg repo The "owner/name" repository (any accepted spelling).
// @arg argsFor Builds the git args from the proxy URL and dir.
// @arg dir The clone destination directory, or "" to let git choose.
// @error error when no base URL or token is configured, or git fails.
//
// @testcase TestGitCloneCommandRunsGit runs git with the built proxy URL and token env.
// @testcase TestGitCloneNeedsToken errors clearly when no subject token is configured.
func runProxyGit(ctx context.Context, a *rscli.App, repo string, argsFor func(url, dir string) []string, dir string) error {
	base := strings.TrimRight(a.BaseURL(), "/")
	if base == "" {
		return fmt.Errorf("no resource server base URL configured; set base_url in the config or pass --base-url")
	}
	token := a.Token()
	if token == "" {
		return fmt.Errorf("no subject token configured; mint one with `granular-subject create` and set token_file or pass --token")
	}
	url := base + "/git/" + githubops.NormalizeRepo(repo) + ".git"
	env := []string{"GRANULAR_SUBJECT_TOKEN=" + token}
	return gitRun(ctx, argsFor(url, dir), env, a.Out)
}

// cloneArgs builds the git arguments for cloning url into dir (when dir is non-empty),
// wiring in the inline credential helper that supplies the subject token.
//
// @arg url The proxy clone URL.
// @arg dir The destination directory, or "" to let git choose.
// @return []string The git argument list.
//
// @testcase TestGitCloneCommandRunsGit checks the clone arguments and URL.
func cloneArgs(url, dir string) []string {
	args := []string{"-c", "credential.helper=", "-c", credentialHelperArg, "clone", url}
	if dir != "" {
		args = append(args, dir)
	}
	return args
}

// pushArgs returns a builder for the git arguments that push refspecs from the working
// directory dir to url. With no refspec given it pushes HEAD.
//
// @arg dir The local repository directory.
// @arg refspecs The refspecs to push (empty pushes HEAD).
// @return func(string, string) []string A builder taking the proxy URL.
//
// @testcase TestGitPushArgsBuildsPush builds a push to the proxy URL with refspecs.
func pushArgs(dir string, refspecs []string) func(url, _ string) []string {
	return func(url, _ string) []string {
		args := []string{"-c", "credential.helper=", "-c", credentialHelperArg, "-C", dir, "push", url}
		if len(refspecs) == 0 {
			return append(args, "HEAD")
		}
		return append(args, refspecs...)
	}
}
