package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/internal/authz"
	"github.com/clems4ever/granular/internal/operations"
)

// TypePullCreate is the operation type id for creating a pull request.
const TypePullCreate = "github.pull.create"

// PullCreateOperation creates a pull request on a GitHub repository server-side
// using the server-held PAT. This is a mutating operation.
type PullCreateOperation struct {
	repo  string
	title string
	head  string
	base  string
	body  string
	draft bool
	token string
}

// PullCreate builds a PullCreateOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo", "title", "head"
// and "base" (required); optional "body" and "draft".
//
// @arg params The wire parameters carrying repo, title, head, base, body and draft.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullCreateOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingTitle if "title" is absent or empty.
// @error ErrMissingBranches if "head" or "base" is absent or empty.
//
// @testcase TestPullCreateFactoryValidatesParams fails on missing repo, title or branches.
// @testcase TestPullCreateRequirementsAreContentScoped builds and checks the key.
func PullCreate(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	title := stringParam(params, "title")
	if title == "" {
		return nil, ErrMissingTitle
	}
	head := stringParam(params, "head")
	base := stringParam(params, "base")
	if head == "" || base == "" {
		return nil, ErrMissingBranches
	}
	return &PullCreateOperation{
		repo:  NormalizeRepo(repo),
		title: title,
		head:  head,
		base:  base,
		body:  stringParam(params, "body"),
		draft: boolParam(params, "draft"),
		token: env.GitHubToken,
	}, nil
}

// Type returns the github.pull.create type id.
//
// @return string The constant TypePullCreate.
//
// @testcase TestPullCreateRequirementsAreContentScoped exercises a built operation.
func (o *PullCreateOperation) Type() string { return TypePullCreate }

// Requirements authorizes creating a pull request in the repository, qualified by a
// hash of the exact content (title, body, head, base, draft), so approving one PR
// does not authorise creating a different one.
//
// @return []authz.Requirement A single pull.create requirement, context-scoped to the content.
//
// @testcase TestPullCreateRequirementsAreContentScoped checks the content hash context.
func (o *PullCreateOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{
		Action:   "pull.create",
		Resource: authz.RepoRef(o.repo),
		Context:  map[string]string{"content_hash": contentHash(o.title, o.body, o.head, o.base, fmt.Sprint(o.draft))},
	}}
}

// Describe returns a human summary for the approval page, including the title and
// the branches so the approver sees what will be opened.
//
// @return string A sentence describing the pull request to be created.
//
// @testcase TestPullCreateDescribe checks the repo, title and branches appear.
func (o *PullCreateOperation) Describe() string {
	kind := "pull request"
	if o.draft {
		kind = "draft pull request"
	}
	summary := fmt.Sprintf("Open a %s in GitHub repository %s titled %q (%s → %s)", kind, o.repo, o.title, o.head, o.base)
	if o.body != "" {
		summary += fmt.Sprintf(":\n\n%s", o.body)
	}
	return summary
}

// Execute creates the pull request via the GitHub REST API and returns GitHub's
// created pull request object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw created pull request object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestPullCreateExecutePosts creates a pull request against a stub API.
func (o *PullCreateOperation) Execute(ctx context.Context) (map[string]any, error) {
	payload := map[string]any{"title": o.title, "head": o.head, "base": o.base}
	if o.body != "" {
		payload["body"] = o.body
	}
	if o.draft {
		payload["draft"] = true
	}

	endpoint := fmt.Sprintf("%s/repos/%s/pulls", apiBaseURL, o.repo)
	var created map[string]any
	if err := postJSON(ctx, o.token, endpoint, payload, &created); err != nil {
		return nil, fmt.Errorf("create pull request: %w", err)
	}
	return created, nil
}

// ErrMissingBranches is returned by PullCreate when the head or base branch is
// missing.
var ErrMissingBranches = fmt.Errorf("missing required parameters: head and base branches")
