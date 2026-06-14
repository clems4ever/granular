package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/internal/operations"
)

// TypeIssueComment is the operation type id for posting a comment on an issue.
const TypeIssueComment = "github.issue.comment"

// IssueCommentOperation posts a comment on a GitHub issue server-side using the
// server-held PAT. This is a mutating operation.
type IssueCommentOperation struct {
	repo   string
	number int
	body   string
	token  string
}

// IssueComment builds an IssueCommentOperation from request parameters and the
// server Env. It satisfies operations.Factory. Expected params: "repo" (required),
// "number" (required, the issue number) and "body" (required, the comment text).
//
// @arg params The wire parameters carrying repo, number and body.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueCommentOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingIssueNumber if "number" is absent or not positive.
// @error ErrMissingBody if "body" is absent or empty.
//
// @testcase TestIssueCommentFactoryValidatesParams fails on missing repo, number or body.
// @testcase TestIssueCommentPermissionKeyIsContentScoped builds and checks the key.
func IssueComment(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingIssueNumber
	}
	body := stringParam(params, "body")
	if body == "" {
		return nil, ErrMissingBody
	}
	return &IssueCommentOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		body:   body,
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.issue.comment type id.
//
// @return string The constant TypeIssueComment.
//
// @testcase TestIssueCommentPermissionKeyIsContentScoped exercises a built operation.
func (o *IssueCommentOperation) Type() string { return TypeIssueComment }

// PermissionKey returns a grant key scoped to the issue and the exact comment
// content, so approving one comment does not authorise posting a different one.
//
// @return string A key of the form "github.issue.comment:<owner/name>#<number>:<hash>".
//
// @testcase TestIssueCommentPermissionKeyIsContentScoped checks the key changes with the body.
func (o *IssueCommentOperation) PermissionKey() string {
	return fmt.Sprintf("%s:%s#%d:%s", TypeIssueComment, o.repo, o.number, contentHash(o.body))
}

// Describe returns a human summary for the approval page, including the comment
// text so the approver sees exactly what will be posted.
//
// @return string A sentence describing the comment to be posted.
//
// @testcase TestIssueCommentDescribe checks the repo, number and body appear.
func (o *IssueCommentOperation) Describe() string {
	return fmt.Sprintf("Post this comment on issue #%d of GitHub repository %s:\n\n%s", o.number, o.repo, o.body)
}

// Execute posts the comment to the GitHub REST API and returns GitHub's created
// comment object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw created-comment object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestIssueCommentExecutePosts posts a comment against a stub API.
func (o *IssueCommentOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d/comments", apiBaseURL, o.repo, o.number)
	var created map[string]any
	if err := postJSON(ctx, o.token, endpoint, map[string]any{"body": o.body}, &created); err != nil {
		return nil, fmt.Errorf("post comment: %w", err)
	}
	return created, nil
}

// ErrMissingBody is returned when a write operation is missing its required body.
var ErrMissingBody = fmt.Errorf("missing required parameter: body")
