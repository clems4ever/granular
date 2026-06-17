package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypePullList is the operation type id for listing a repository's pull requests.
const TypePullList = "github.pull.list"

// defaultPullLimit is how many pull requests are listed when no limit is requested.
const defaultPullLimit = 30

// PullListOperation lists the pull requests of a GitHub repository server-side
// using the server-held PAT, returning them to the client.
type PullListOperation struct {
	repo  string
	state string
	limit int
	token string
}

// PullList builds a PullListOperation from request parameters and the server Env.
// It satisfies operations.Factory. Expected params: "repo" (required), "state"
// (optional: open|closed|all, default open), "limit" (optional).
//
// @arg params The wire parameters carrying repo, state and limit.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullListOperation.
// @error ErrMissingRepo if the "repo" parameter is absent or empty.
//
// @testcase TestPullListFactoryRequiresRepo fails when repo is missing.
// @testcase TestPullListRequirements builds and checks the requirement.
func PullList(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	state := stringParam(params, "state")
	if state == "" {
		state = "open"
	}
	return &PullListOperation{
		repo:  NormalizeRepo(repo),
		state: state,
		limit: intParam(params, "limit", defaultPullLimit),
		token: env.GitHubToken,
	}, nil
}

// Type returns the github.pull.list type id.
//
// @return string The constant TypePullList.
//
// @testcase TestPullListRequirements exercises a built operation.
func (o *PullListOperation) Type() string { return TypePullList }

// Requirements authorizes listing the repository's pull requests (a repo-scoped
// read; the state is a query filter, not part of the grant scope).
//
// @return []authz.Requirement A single pull.list requirement on the repository.
//
// @testcase TestPullListRequirements checks the action and resource.
func (o *PullListOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "pull.list", Resource: authz.RepoRef(o.repo)}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the listing to be approved.
//
// @testcase TestPullListDescribe checks the repo and state appear in the summary.
func (o *PullListOperation) Describe() string {
	return fmt.Sprintf("List %s pull requests of GitHub repository %s", o.state, o.repo)
}

// Execute calls the GitHub REST API to list the repository's pull requests and
// returns GitHub's response verbatim (every attribute, every item) under "pulls".
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result with "pulls" set to GitHub's raw pull request array.
// @error error when the request cannot be built, the call fails, or GitHub returns non-200.
//
// @testcase TestPullListExecuteReturnsRaw lists pull requests against a stub API.
func (o *PullListOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/pulls?state=%s&per_page=%s",
		apiBaseURL, o.repo, url.QueryEscape(o.state), strconv.Itoa(o.limit))

	var pulls []any
	if err := getJSON(ctx, o.token, endpoint, &pulls); err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}
	return map[string]any{"pulls": pulls}, nil
}
