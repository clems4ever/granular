// Package github implements granular operations targeting GitHub. The first is
// github.clone: it does not clone server-side. Instead, once approved, it hands
// the client a resource-server-relative clone path pointing at the server's git
// proxy, which injects the server-held PAT. The actual clone runs on the client,
// against the resource server URL it already uses.
package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypeClone is the operation type id for cloning a GitHub repository.
const TypeClone = "github.clone"

// CloneOperation authorises cloning a single GitHub repository through the server
// git proxy. Grants are scoped to the whole repository.
type CloneOperation struct {
	repo string
}

// Clone builds a CloneOperation from request parameters and the server Env. It
// satisfies operations.Factory. Expected params: "repo" (required, e.g.
// "owner/name").
//
// @arg params The wire parameters carrying repo.
// @arg env The server Env (unused: the clone path is resource-server-relative).
// @return operations.Operation A ready-to-authorise CloneOperation.
// @error ErrMissingRepo if the "repo" parameter is absent or empty.
//
// @testcase TestCloneFactoryRequiresRepo fails when repo is missing.
// @testcase TestCloneRequirements builds successfully and checks the key.
func Clone(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	return &CloneOperation{repo: NormalizeRepo(repo)}, nil
}

// Type returns the github.clone type id.
//
// @return string The constant TypeClone.
//
// @testcase TestCloneRequirements exercises a built operation.
func (o *CloneOperation) Type() string { return TypeClone }

// Requirements authorizes cloning the repository.
//
// @return []authz.Requirement A single repo.clone requirement on the repository.
//
// @testcase TestCloneRequirements checks the action and resource.
func (o *CloneOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "repo.clone", Resource: authz.RepoRef(o.repo)}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the clone to be approved.
//
// @testcase TestCloneDescribe checks the repo appears in the summary.
func (o *CloneOperation) Describe() string {
	return fmt.Sprintf("Clone GitHub repository %s through the granular proxy", o.repo)
}

// Execute does no server-side work: it returns the resource-server-relative clone
// path the client should clone from. The client joins it with the resource server
// URL it already uses, and the request routes back through the server's
// authenticating git proxy (which injects the server-held PAT).
//
// @arg ctx Context (unused; the clone happens on the client).
// @return map[string]any Result with a relative "clone_path" and the "repo".
// @error error is always nil; the signature matches operations.Operation.
//
// @testcase TestExecuteReturnsProxyClonePath checks the relative path is built.
func (o *CloneOperation) Execute(ctx context.Context) (map[string]any, error) {
	return map[string]any{
		"clone_path": "/git/" + o.repo + ".git",
		"repo":       o.repo,
	}, nil
}

// NormalizeRepo reduces the many accepted repo spellings to a bare "owner/name".
//
// @arg repo A repo reference such as a URL, host path, or "owner/name".
// @return string The "owner/name" form with any scheme, host and .git suffix stripped.
//
// @testcase TestNormalizeRepo checks URL, host-prefixed and bare forms.
func NormalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "git@github.com:")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimPrefix(repo, "/")
	repo = strings.TrimSuffix(repo, ".git")
	return repo
}

// stringParam reads a string-valued parameter, returning "" when absent or not a
// string.
//
// @arg params The parameter map from the wire request.
// @arg key The parameter name to read.
// @return string The trimmed string value, or "" if missing or non-string.
//
// @testcase TestCloneFactoryRequiresRepo exercises the missing-key path.
func stringParam(params map[string]any, key string) string {
	v, ok := params[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// ErrMissingRepo is returned by Clone when no repo parameter is supplied.
var ErrMissingRepo = fmt.Errorf("missing required parameter: repo")
