package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypePullMerge is the operation type id for merging a pull request.
const TypePullMerge = "github.pull.merge"

// PullMergeOperation merges a GitHub pull request server-side using the server-held
// PAT. This is a mutating operation.
type PullMergeOperation struct {
	repo   string
	number int
	method string
	sha    string
	token  string
}

// PullMerge builds a PullMergeOperation from request parameters and the server Env.
// It satisfies operations.Factory. Expected params: "repo" and "number"
// (required); optional "method" (merge|squash|rebase, default merge) and "sha"
// (the expected head SHA the merge must match).
//
// @arg params The wire parameters carrying repo, number, method and sha.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullMergeOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
// @error ErrInvalidMergeMethod if "method" is set to an unsupported value.
//
// @testcase TestPullMergeFactoryValidatesParams fails on bad params or method.
// @testcase TestPullMergeRequirements builds and checks the requirement.
func PullMerge(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}
	method, ok := normalizeMergeMethod(stringParam(params, "method"))
	if !ok {
		return nil, ErrInvalidMergeMethod
	}
	return &PullMergeOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		method: method,
		sha:    stringParam(params, "sha"),
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.pull.merge type id.
//
// @return string The constant TypePullMerge.
//
// @testcase TestPullMergeRequirements exercises a built operation.
func (o *PullMergeOperation) Type() string { return TypePullMerge }

// Requirements authorizes merging the pull request, qualified by the merge method
// (and the expected head SHA when supplied) so approving one merge does not
// authorise a different strategy.
//
// @return []authz.Requirement A single pull.merge requirement, context-scoped to the method and sha.
//
// @testcase TestPullMergeRequirements checks the action, resource and context.
func (o *PullMergeOperation) Requirements() []authz.Requirement {
	ctx := map[string]string{"method": o.method}
	if o.sha != "" {
		ctx["sha"] = o.sha
	}
	return []authz.Requirement{{
		Action:   "pull.merge",
		Resource: authz.PullRef(o.repo, o.number),
		Context:  ctx,
	}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the merge to be approved.
//
// @testcase TestPullMergeDescribe checks the repo, number and method appear.
func (o *PullMergeOperation) Describe() string {
	return fmt.Sprintf("Merge pull request #%d of GitHub repository %s using the %s method", o.number, o.repo, o.method)
}

// Execute merges the pull request via the GitHub REST API and returns GitHub's raw
// merge result object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw merge-result object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestPullMergeExecutePuts merges a pull request against a stub API.
func (o *PullMergeOperation) Execute(ctx context.Context) (map[string]any, error) {
	payload := map[string]any{"merge_method": o.method}
	if o.sha != "" {
		payload["sha"] = o.sha
	}
	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d/merge", apiBaseURL, o.repo, o.number)
	var result map[string]any
	if err := putJSON(ctx, o.token, endpoint, payload, &result); err != nil {
		return nil, fmt.Errorf("merge pull request: %w", err)
	}
	return result, nil
}

// normalizeMergeMethod maps a user-supplied merge method to the GitHub
// merge_method value, reporting whether it is valid. An empty value defaults to
// "merge".
//
// @arg raw The raw method from the request ("", merge, squash, rebase).
// @return string The GitHub merge_method value.
// @return bool True when the method is empty or recognised.
//
// @testcase TestPullMergeFactoryValidatesParams exercises valid and invalid methods.
func normalizeMergeMethod(raw string) (string, bool) {
	switch raw {
	case "":
		return "merge", true
	case "merge", "squash", "rebase":
		return raw, true
	default:
		return "", false
	}
}

// ErrInvalidMergeMethod is returned when a merge method is not merge, squash or
// rebase.
var ErrInvalidMergeMethod = fmt.Errorf(`invalid merge method (want "merge", "squash" or "rebase")`)
