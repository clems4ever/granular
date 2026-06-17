package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypePullComment is the operation type id for posting a comment on a pull request.
const TypePullComment = "github.pull.comment"

// PullCommentOperation posts a conversation comment on a GitHub pull request
// server-side using the server-held PAT. Pull request conversation comments use
// the issues comments endpoint. This is a mutating operation.
type PullCommentOperation struct {
	repo   string
	number int
	body   string
	token  string
}

// PullComment builds a PullCommentOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" (required),
// "number" (required, the PR number) and "body" (required, the comment text).
//
// @arg params The wire parameters carrying repo, number and body.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullCommentOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
// @error ErrMissingBody if "body" is absent or empty.
//
// @testcase TestPullCommentFactoryValidatesParams fails on missing repo, number or body.
// @testcase TestPullCommentRequirementsAreContentScoped builds and checks the key.
func PullComment(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}
	body := stringParam(params, "body")
	if body == "" {
		return nil, ErrMissingBody
	}
	return &PullCommentOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		body:   body,
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.pull.comment type id.
//
// @return string The constant TypePullComment.
//
// @testcase TestPullCommentRequirementsAreContentScoped exercises a built operation.
func (o *PullCommentOperation) Type() string { return TypePullComment }

// Requirements authorizes posting the comment on the pull request, qualified by a
// hash of the exact body so approving one comment does not authorise posting a
// different one.
//
// @return []authz.Requirement A single pull.comment requirement, context-scoped to the body.
//
// @testcase TestPullCommentRequirementsAreContentScoped checks the body hash context.
func (o *PullCommentOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{
		Action:   "pull.comment",
		Resource: authz.PullRef(o.repo, o.number),
		Context:  map[string]string{"body_hash": contentHash(o.body)},
	}}
}

// Describe returns a human summary for the approval page, including the comment
// text so the approver sees exactly what will be posted.
//
// @return string A sentence describing the comment to be posted.
//
// @testcase TestPullCommentDescribe checks the repo, number and body appear.
func (o *PullCommentOperation) Describe() string {
	return fmt.Sprintf("Post this comment on pull request #%d of GitHub repository %s:\n\n%s", o.number, o.repo, o.body)
}

// Execute posts the comment to the GitHub REST API and returns GitHub's created
// comment object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw created-comment object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestPullCommentExecutePosts posts a comment against a stub API.
func (o *PullCommentOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d/comments", apiBaseURL, o.repo, o.number)
	var created map[string]any
	if err := postJSON(ctx, o.token, endpoint, map[string]any{"body": o.body}, &created); err != nil {
		return nil, fmt.Errorf("post pull request comment: %w", err)
	}
	return created, nil
}
