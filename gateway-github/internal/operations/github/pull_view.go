package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/gateway-github/internal/authz"
	"github.com/clems4ever/granular/gateway-github/internal/operations"
)

// TypePullView and TypePullDiff are the operation type ids for viewing a single
// pull request and for fetching its unified diff.
const (
	TypePullView = "github.pull.view"
	TypePullDiff = "github.pull.diff"
)

// PullViewOperation fetches the details of a single GitHub pull request
// server-side using the server-held PAT, optionally including its conversation
// (issue comments, review comments and reviews).
type PullViewOperation struct {
	repo     string
	number   int
	comments bool
	token    string
}

// PullView builds a PullViewOperation from request parameters and the server Env.
// It satisfies operations.Factory. Expected params: "repo" (required), "number"
// (required, the PR number) and "comments" (optional bool).
//
// @arg params The wire parameters carrying repo, number and comments.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullViewOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
//
// @testcase TestPullViewFactoryValidatesParams fails on missing repo or number.
// @testcase TestPullViewCommentsAddsRequirement checks --comments adds comment.read.
func PullView(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}
	return &PullViewOperation{
		repo:     NormalizeRepo(repo),
		number:   number,
		comments: boolParam(params, "comments"),
		token:    env.GitHubToken,
	}, nil
}

// Type returns the github.pull.view type id.
//
// @return string The constant TypePullView.
//
// @testcase TestPullViewRequirements exercises a built operation.
func (o *PullViewOperation) Type() string { return TypePullView }

// Requirements authorizes viewing the pull request; when comments are requested it
// adds a comment.read requirement so reading the conversation is approved
// separately from the PR metadata.
//
// @return []authz.Requirement A pull.view requirement, plus comment.read when comments are requested.
//
// @testcase TestPullViewRequirements checks the base requirement.
// @testcase TestPullViewCommentsAddsRequirement checks --comments adds comment.read.
func (o *PullViewOperation) Requirements() []authz.Requirement {
	reqs := []authz.Requirement{{Action: "pull.view", Resource: authz.PullRef(o.repo, o.number)}}
	if o.comments {
		reqs = append(reqs, authz.Requirement{Action: "comment.read", Resource: authz.PullRef(o.repo, o.number)})
	}
	return reqs
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the pull request to be viewed.
//
// @testcase TestPullViewDescribe checks the repo and number appear in the summary.
func (o *PullViewOperation) Describe() string {
	if o.comments {
		return fmt.Sprintf("View pull request #%d (with conversation) of GitHub repository %s", o.number, o.repo)
	}
	return fmt.Sprintf("View pull request #%d of GitHub repository %s", o.number, o.repo)
}

// Execute fetches the pull request (and, when requested, its conversation) from
// the GitHub REST API and returns GitHub's raw pull request object. When comments
// are requested the raw issue comments, review comments and reviews arrays are
// added under the synthetic "comments_list", "review_comments_list" and
// "reviews_list" keys.
//
// @arg ctx Context for cancellation of the API calls.
// @return map[string]any Result set to GitHub's raw pull request object, plus the conversation arrays when requested.
// @error error when a request fails or GitHub returns a non-200 status.
//
// @testcase TestPullViewExecuteReturnsRaw fetches a pull request against a stub API.
// @testcase TestPullViewExecuteWithComments also fetches the conversation endpoints.
func (o *PullViewOperation) Execute(ctx context.Context) (map[string]any, error) {
	var pull map[string]any
	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d", apiBaseURL, o.repo, o.number)
	if err := getJSON(ctx, o.token, endpoint, &pull); err != nil {
		return nil, fmt.Errorf("view pull request: %w", err)
	}

	if o.comments {
		issueComments := fmt.Sprintf("%s/repos/%s/issues/%d/comments", apiBaseURL, o.repo, o.number)
		var comments []any
		if err := getJSON(ctx, o.token, issueComments, &comments); err != nil {
			return nil, fmt.Errorf("list pull request comments: %w", err)
		}
		pull["comments_list"] = comments

		var reviewComments []any
		if err := getJSON(ctx, o.token, endpoint+"/comments", &reviewComments); err != nil {
			return nil, fmt.Errorf("list pull request review comments: %w", err)
		}
		pull["review_comments_list"] = reviewComments

		var reviews []any
		if err := getJSON(ctx, o.token, endpoint+"/reviews", &reviews); err != nil {
			return nil, fmt.Errorf("list pull request reviews: %w", err)
		}
		pull["reviews_list"] = reviews
	}
	return pull, nil
}

// PullDiffOperation fetches the unified diff of a single GitHub pull request
// server-side using the server-held PAT.
type PullDiffOperation struct {
	repo   string
	number int
	token  string
}

// PullDiff builds a PullDiffOperation from request parameters and the server Env.
// It satisfies operations.Factory. Expected params: "repo" and "number" (required).
//
// @arg params The wire parameters carrying repo and number.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullDiffOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
//
// @testcase TestPullDiffFactoryValidatesParams fails on missing repo or number.
// @testcase TestPullDiffRequirements builds and checks the requirement.
func PullDiff(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}
	return &PullDiffOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.pull.diff type id.
//
// @return string The constant TypePullDiff.
//
// @testcase TestPullDiffRequirements exercises a built operation.
func (o *PullDiffOperation) Type() string { return TypePullDiff }

// Requirements authorizes fetching the pull request's diff.
//
// @return []authz.Requirement A single pull.diff requirement on the pull request.
//
// @testcase TestPullDiffRequirements checks the action and resource.
func (o *PullDiffOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "pull.diff", Resource: authz.PullRef(o.repo, o.number)}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the diff to be fetched.
//
// @testcase TestPullDiffDescribe checks the repo and number appear in the summary.
func (o *PullDiffOperation) Describe() string {
	return fmt.Sprintf("View the diff of pull request #%d of GitHub repository %s", o.number, o.repo)
}

// Execute fetches the pull request's unified diff from the GitHub REST API (using
// the diff media type) and returns it verbatim under "diff".
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result with "diff" set to GitHub's raw unified diff text.
// @error error when the request fails or GitHub returns a non-200 status.
//
// @testcase TestPullDiffExecuteReturnsRaw fetches a diff against a stub API.
func (o *PullDiffOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d", apiBaseURL, o.repo, o.number)
	diff, err := getRaw(ctx, o.token, endpoint, "application/vnd.github.diff")
	if err != nil {
		return nil, fmt.Errorf("view pull request diff: %w", err)
	}
	return map[string]any{"diff": diff}, nil
}

// ErrMissingPullNumber is returned by pull operations when no positive PR number is
// supplied.
var ErrMissingPullNumber = fmt.Errorf("missing or invalid required parameter: number")
