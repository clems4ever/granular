package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypePush is the operation type id for pushing to a GitHub repository.
const TypePush = "github.push"

// PushOperation authorises pushing to a single GitHub repository through the server
// git proxy. Like clone, the push itself runs on the client against the brokered
// proxy URL, which injects the server-held PAT. Grants are scoped to the whole
// repository.
type PushOperation struct {
	repo    string
	baseURL string
}

// Push builds a PushOperation from request parameters and the server Env. It
// satisfies operations.Factory. Expected params: "repo" (required, e.g.
// "owner/name").
//
// @arg params The wire parameters carrying repo.
// @arg env The server Env supplying the public base URL.
// @return operations.Operation A ready-to-authorise PushOperation.
// @error ErrMissingRepo if the "repo" parameter is absent or empty.
//
// @testcase TestPushFactoryRequiresRepo fails when repo is missing.
// @testcase TestPushRequirements builds successfully and checks the requirement.
func Push(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	return &PushOperation{repo: NormalizeRepo(repo), baseURL: env.BaseURL}, nil
}

// Type returns the github.push type id.
//
// @return string The constant TypePush.
//
// @testcase TestPushRequirements exercises a built operation.
func (o *PushOperation) Type() string { return TypePush }

// Requirements authorizes pushing to the repository through the git proxy.
//
// @return []authz.Requirement A single repo.push requirement on the repository.
//
// @testcase TestPushRequirements checks the action and resource.
func (o *PushOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "repo.push", Resource: authz.RepoRef(o.repo)}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the push to be approved.
//
// @testcase TestPushDescribe checks the repo appears in the summary.
func (o *PushOperation) Describe() string {
	return fmt.Sprintf("Push to GitHub repository %s through the granular proxy", o.repo)
}

// Execute does no server-side work: it returns the brokered push URL the client
// should push to, which routes back through the server's authenticating git proxy.
//
// @arg ctx Context (unused; the push happens on the client).
// @return map[string]any Result with a "push_url" and the "repo".
// @error error is always nil; the signature matches operations.Operation.
//
// @testcase TestPushExecuteReturnsProxyURL checks the brokered URL is built.
func (o *PushOperation) Execute(ctx context.Context) (map[string]any, error) {
	return map[string]any{
		"push_url": strings.TrimRight(o.baseURL, "/") + "/git/" + o.repo + ".git",
		"repo":     o.repo,
	}, nil
}
