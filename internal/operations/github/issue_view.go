package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/internal/authz"
	"github.com/clems4ever/granular/internal/operations"
)

// TypeIssueView is the operation type id for viewing a single issue's details.
const TypeIssueView = "github.issue.view"

// IssueViewOperation fetches the details of a single GitHub issue server-side
// using the server-held PAT, optionally including the issue's comments.
type IssueViewOperation struct {
	repo     string
	number   int
	comments bool
	token    string
}

// IssueView builds an IssueViewOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" (required),
// "number" (required, the issue number) and "comments" (optional bool).
//
// @arg params The wire parameters carrying repo, number and comments.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueViewOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingIssueNumber if "number" is absent or not positive.
//
// @testcase TestIssueViewFactoryValidatesParams fails on missing repo or number.
// @testcase TestIssueViewPermissionKeyIncludesNumber builds and checks the key.
func IssueView(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingIssueNumber
	}
	return &IssueViewOperation{
		repo:     NormalizeRepo(repo),
		number:   number,
		comments: boolParam(params, "comments"),
		token:    env.GitHubToken,
	}, nil
}

// Type returns the github.issue.view type id.
//
// @return string The constant TypeIssueView.
//
// @testcase TestIssueViewPermissionKeyIncludesNumber exercises a built operation.
func (o *IssueViewOperation) Type() string { return TypeIssueView }

// PermissionKey returns a grant key scoped to the specific repository and issue
// number; when comments are requested it adds a second comment.read requirement,
// so viewing an issue's discussion is approved separately from its metadata.
//
// @return []authz.Requirement An issue.view requirement, plus comment.read when comments are requested.
//
// @testcase TestIssueViewRequirements checks the base requirement.
// @testcase TestIssueViewCommentsAddsRequirement checks --comments adds comment.read.
func (o *IssueViewOperation) Requirements() []authz.Requirement {
	reqs := []authz.Requirement{{Action: "issue.view", Resource: authz.IssueRef(o.repo, o.number)}}
	if o.comments {
		reqs = append(reqs, authz.Requirement{Action: "comment.read", Resource: authz.IssueRef(o.repo, o.number)})
	}
	return reqs
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the issue to be viewed.
//
// @testcase TestIssueViewDescribe checks the repo and number appear in the summary.
func (o *IssueViewOperation) Describe() string {
	if o.comments {
		return fmt.Sprintf("View issue #%d (with comments) of GitHub repository %s", o.number, o.repo)
	}
	return fmt.Sprintf("View issue #%d of GitHub repository %s", o.number, o.repo)
}

// Execute fetches the issue (and, when requested, its comments) from the GitHub
// REST API and returns GitHub's raw issue object. When comments are requested the
// raw comments array is added under the synthetic "comments_list" key.
//
// @arg ctx Context for cancellation of the API calls.
// @return map[string]any Result set to GitHub's raw issue object, plus "comments_list" when requested.
// @error error when a request fails or GitHub returns a non-200 status.
//
// @testcase TestIssueViewExecuteReturnsRaw fetches an issue against a stub API.
// @testcase TestIssueViewExecuteWithComments also fetches the comments endpoint.
func (o *IssueViewOperation) Execute(ctx context.Context) (map[string]any, error) {
	var issue map[string]any
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d", apiBaseURL, o.repo, o.number)
	if err := getJSON(ctx, o.token, endpoint, &issue); err != nil {
		return nil, fmt.Errorf("view issue: %w", err)
	}

	if o.comments {
		var comments []any
		if err := getJSON(ctx, o.token, endpoint+"/comments", &comments); err != nil {
			return nil, fmt.Errorf("list issue comments: %w", err)
		}
		issue["comments_list"] = comments
	}
	return issue, nil
}

// ErrMissingIssueNumber is returned by IssueView when no positive issue number is
// supplied.
var ErrMissingIssueNumber = fmt.Errorf("missing or invalid required parameter: number")
