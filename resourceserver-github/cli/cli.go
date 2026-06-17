// Package githubcli builds the granular GitHub resource-server CLI. It declares
// the GitHub operations as typed rscli commands and lets the rscli SDK assemble
// the full command tree — the GitHub operation commands plus the built-in
// `catalog` and `sign` commands every resource-server CLI carries.
package githubcli

import (
	"io"

	githubops "github.com/clems4ever/granular/resourceserver-github/internal/operations/github"
	"github.com/clems4ever/granular/rscli"
	"github.com/spf13/cobra"
)

// Common flags shared across many GitHub operations.
var (
	fRepo     = rscli.Flag{Name: "repo", Required: true, Usage: "repository, owner/name"}
	fNumber   = rscli.Flag{Name: "number", Type: rscli.IntFlag, Required: true, Usage: "item number"}
	fState    = rscli.Flag{Name: "state", Usage: "filter by state: open|closed|all (default open)"}
	fLimit    = rscli.Flag{Name: "limit", Type: rscli.IntFlag, Usage: "max results (default 30)"}
	fComments = rscli.Flag{Name: "comments", Type: rscli.BoolFlag, Usage: "also fetch comments (needs comment.read)"}
	fBody     = rscli.Flag{Name: "body", Usage: "body text"}
)

// operations declares every GitHub operation as a CLI command. Multi-word paths
// (e.g. "issue create") nest under a shared group command.
//
// @return []rscli.OpCommand The GitHub operation command declarations.
//
// @testcase TestNewRootCmdHasGitHubCommands builds the root from these declarations.
func operations() []rscli.OpCommand {
	return []rscli.OpCommand{
		// clone and push are not generic operations: they run real git through the
		// resource server's authorizing proxy and are wired as Extra commands below.
		{Path: "issue list", Type: githubops.TypeIssueList, Short: "List a repository's issues", Flags: []rscli.Flag{fRepo, fState, fLimit}},
		{Path: "issue view", Type: githubops.TypeIssueView, Short: "View a single issue", Flags: []rscli.Flag{fRepo, fNumber, fComments}},
		{Path: "issue create", Type: githubops.TypeIssueCreate, Short: "Create an issue", Flags: []rscli.Flag{
			fRepo,
			{Name: "title", Required: true, Usage: "issue title"},
			fBody,
			{Name: "labels", Type: rscli.StringSliceFlag, Usage: "labels (repeatable)"},
			{Name: "assignees", Type: rscli.StringSliceFlag, Usage: "assignees (repeatable)"},
		}},
		{Path: "issue comment", Type: githubops.TypeIssueComment, Short: "Comment on an issue", Flags: []rscli.Flag{
			fRepo, fNumber, {Name: "body", Required: true, Usage: "comment body"},
		}},
		{Path: "issue edit", Type: githubops.TypeIssueEdit, Short: "Edit an issue", Flags: []rscli.Flag{
			fRepo, fNumber,
			{Name: "title", Usage: "new title"},
			fBody,
			{Name: "add-labels", Param: "add_labels", Type: rscli.StringSliceFlag, Usage: "labels to add"},
			{Name: "remove-labels", Param: "remove_labels", Type: rscli.StringSliceFlag, Usage: "labels to remove"},
			{Name: "add-assignees", Param: "add_assignees", Type: rscli.StringSliceFlag, Usage: "assignees to add"},
			{Name: "remove-assignees", Param: "remove_assignees", Type: rscli.StringSliceFlag, Usage: "assignees to remove"},
		}},
		{Path: "issue close", Type: githubops.TypeIssueClose, Short: "Close an issue", Flags: []rscli.Flag{
			fRepo, fNumber, {Name: "reason", Usage: "completed|not planned"},
		}},
		{Path: "issue reopen", Type: githubops.TypeIssueReopen, Short: "Reopen a closed issue", Flags: []rscli.Flag{fRepo, fNumber}},

		{Path: "pull list", Type: githubops.TypePullList, Short: "List a repository's pull requests", Flags: []rscli.Flag{fRepo, fState, fLimit}},
		{Path: "pull view", Type: githubops.TypePullView, Short: "View a single pull request", Flags: []rscli.Flag{fRepo, fNumber, fComments}},
		{Path: "pull diff", Type: githubops.TypePullDiff, Short: "View a pull request's diff", Flags: []rscli.Flag{fRepo, fNumber}},
		{Path: "pull create", Type: githubops.TypePullCreate, Short: "Create a pull request", Flags: []rscli.Flag{
			fRepo,
			{Name: "title", Required: true, Usage: "pull request title"},
			{Name: "head", Required: true, Usage: "source branch"},
			{Name: "base", Required: true, Usage: "target branch"},
			fBody,
			{Name: "draft", Type: rscli.BoolFlag, Usage: "create as draft"},
		}},
		{Path: "pull comment", Type: githubops.TypePullComment, Short: "Comment on a pull request", Flags: []rscli.Flag{
			fRepo, fNumber, {Name: "body", Required: true, Usage: "comment body"},
		}},
		{Path: "pull review", Type: githubops.TypePullReview, Short: "Review a pull request", Flags: []rscli.Flag{
			fRepo, fNumber,
			{Name: "event", Required: true, Usage: "approve|request_changes|comment"},
			{Name: "body", Usage: "review body (required unless approve)"},
		}},
		{Path: "pull edit", Type: githubops.TypePullEdit, Short: "Edit a pull request", Flags: []rscli.Flag{
			fRepo, fNumber,
			{Name: "title", Usage: "new title"},
			fBody,
			{Name: "base", Usage: "new base branch"},
		}},
		{Path: "pull merge", Type: githubops.TypePullMerge, Short: "Merge a pull request", Flags: []rscli.Flag{
			fRepo, fNumber,
			{Name: "method", Usage: "merge|squash|rebase (default merge)"},
			{Name: "sha", Usage: "expected head SHA"},
		}},
		{Path: "pull close", Type: githubops.TypePullClose, Short: "Close a pull request", Flags: []rscli.Flag{fRepo, fNumber}},
		{Path: "pull reopen", Type: githubops.TypePullReopen, Short: "Reopen a closed pull request", Flags: []rscli.Flag{fRepo, fNumber}},
	}
}

// NewRootCmd builds the GitHub resource-server CLI root command, writing output
// to out. It wires the GitHub operation commands together with rscli's built-in
// catalog and sign commands.
//
// @arg out The writer command output is written to.
// @return *cobra.Command The root command, ready to Execute.
//
// @testcase TestNewRootCmdHasGitHubCommands checks the GitHub and built-in commands are present.
func NewRootCmd(out io.Writer) *cobra.Command {
	return rscli.NewRootCmd(rscli.Spec{
		Use:            "granular-github",
		Short:          "Catalog, request, and run operations on the granular GitHub resource server",
		RSID:           "github",
		DefaultBaseURL: "http://localhost:9091",
		Operations:     operations(),
		Extra:          gitCommands,
	}, out)
}
