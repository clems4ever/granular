package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypePullClose and TypePullReopen are the operation type ids for changing a pull
// request's open/closed status. They are deliberately separate from pull.edit so a
// grant to change status cannot also edit the pull request's content.
const (
	TypePullClose  = "github.pull.close"
	TypePullReopen = "github.pull.reopen"
)

// PullCloseOperation closes a GitHub pull request server-side. This is a mutating
// operation.
type PullCloseOperation struct {
	repo   string
	number int
	token  string
}

// PullClose builds a PullCloseOperation from request parameters and the server Env.
// It satisfies operations.Factory. Expected params: "repo" and "number" (required).
//
// @arg params The wire parameters carrying repo and number.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullCloseOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
//
// @testcase TestPullCloseFactoryValidatesParams fails on missing repo or number.
// @testcase TestPullCloseRequirements builds and checks the requirement.
func PullClose(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}
	return &PullCloseOperation{repo: NormalizeRepo(repo), number: number, token: env.GitHubToken}, nil
}

// Type returns the github.pull.close type id.
//
// @return string The constant TypePullClose.
//
// @testcase TestPullCloseRequirements exercises a built operation.
func (o *PullCloseOperation) Type() string { return TypePullClose }

// Requirements authorizes closing the pull request.
//
// @return []authz.Requirement A single pull.close requirement on the pull request.
//
// @testcase TestPullCloseRequirements checks the action and resource.
func (o *PullCloseOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "pull.close", Resource: authz.PullRef(o.repo, o.number)}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the close to be approved.
//
// @testcase TestPullCloseDescribe checks the repo and number appear.
func (o *PullCloseOperation) Describe() string {
	return fmt.Sprintf("Close pull request #%d of GitHub repository %s", o.number, o.repo)
}

// Execute closes the pull request via the GitHub REST API and returns the updated
// pull request object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw updated pull request object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestPullCloseExecutePatches closes a pull request against a stub API.
func (o *PullCloseOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d", apiBaseURL, o.repo, o.number)
	var updated map[string]any
	if err := patchJSON(ctx, o.token, endpoint, map[string]any{"state": "closed"}, &updated); err != nil {
		return nil, fmt.Errorf("close pull request: %w", err)
	}
	return updated, nil
}

// PullReopenOperation reopens a closed GitHub pull request server-side. This is a
// mutating operation.
type PullReopenOperation struct {
	repo   string
	number int
	token  string
}

// PullReopen builds a PullReopenOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" and "number"
// (required).
//
// @arg params The wire parameters carrying repo and number.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullReopenOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
//
// @testcase TestPullReopenFactoryValidatesParams fails on missing repo or number.
// @testcase TestPullReopenRequirements builds and checks the requirement.
func PullReopen(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}
	return &PullReopenOperation{repo: NormalizeRepo(repo), number: number, token: env.GitHubToken}, nil
}

// Type returns the github.pull.reopen type id.
//
// @return string The constant TypePullReopen.
//
// @testcase TestPullReopenRequirements exercises a built operation.
func (o *PullReopenOperation) Type() string { return TypePullReopen }

// Requirements authorizes reopening the pull request.
//
// @return []authz.Requirement A single pull.reopen requirement on the pull request.
//
// @testcase TestPullReopenRequirements checks the action and resource.
func (o *PullReopenOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "pull.reopen", Resource: authz.PullRef(o.repo, o.number)}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the reopen to be approved.
//
// @testcase TestPullReopenDescribe checks the repo and number appear.
func (o *PullReopenOperation) Describe() string {
	return fmt.Sprintf("Reopen pull request #%d of GitHub repository %s", o.number, o.repo)
}

// Execute reopens the pull request via the GitHub REST API and returns the updated
// pull request object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw updated pull request object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestPullReopenExecutePatches reopens a pull request against a stub API.
func (o *PullReopenOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d", apiBaseURL, o.repo, o.number)
	var updated map[string]any
	if err := patchJSON(ctx, o.token, endpoint, map[string]any{"state": "open"}, &updated); err != nil {
		return nil, fmt.Errorf("reopen pull request: %w", err)
	}
	return updated, nil
}
