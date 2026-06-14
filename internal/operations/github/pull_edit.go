package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/clems4ever/granular/internal/authz"
	"github.com/clems4ever/granular/internal/operations"
)

// TypePullEdit is the operation type id for editing a pull request's fields (not
// its open/closed status, which is github.pull.close / github.pull.reopen).
const TypePullEdit = "github.pull.edit"

// PullEditOperation edits a GitHub pull request's fields (title, body, base
// branch) server-side. This is a mutating operation.
type PullEditOperation struct {
	repo     string
	number   int
	title    string
	titleSet bool
	body     string
	bodySet  bool
	base     string
	baseSet  bool
	token    string
}

// PullEdit builds a PullEditOperation from request parameters and the server Env.
// It satisfies operations.Factory. Expected params: "repo" and "number"
// (required); optional "title", "body", "base". At least one change is required.
//
// @arg params The wire parameters carrying repo, number and the field changes.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute PullEditOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingPullNumber if "number" is absent or not positive.
// @error ErrNoChanges if no field change is requested.
//
// @testcase TestPullEditFactoryValidatesParams fails on bad params or no changes.
// @testcase TestPullEditRequirementsAreContentScoped builds and checks the key.
func PullEdit(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingPullNumber
	}

	title, titleSet := params["title"].(string)
	body, bodySet := params["body"].(string)
	base, baseSet := params["base"].(string)
	op := &PullEditOperation{
		repo:     NormalizeRepo(repo),
		number:   number,
		title:    title,
		titleSet: titleSet,
		body:     body,
		bodySet:  bodySet,
		base:     base,
		baseSet:  baseSet,
		token:    env.GitHubToken,
	}
	if !op.titleSet && !op.bodySet && !op.baseSet {
		return nil, ErrNoChanges
	}
	return op, nil
}

// Type returns the github.pull.edit type id.
//
// @return string The constant TypePullEdit.
//
// @testcase TestPullEditRequirementsAreContentScoped exercises a built operation.
func (o *PullEditOperation) Type() string { return TypePullEdit }

// Requirements authorizes editing the pull request, qualified by a hash of the
// exact set of changes, so approving one edit does not authorise a different one.
//
// @return []authz.Requirement A single pull.edit requirement, context-scoped to the change set.
//
// @testcase TestPullEditRequirementsAreContentScoped checks the change-set hash context.
func (o *PullEditOperation) Requirements() []authz.Requirement {
	var parts []string
	if o.titleSet {
		parts = append(parts, "title="+o.title)
	}
	if o.bodySet {
		parts = append(parts, "body="+o.body)
	}
	if o.baseSet {
		parts = append(parts, "base="+o.base)
	}
	return []authz.Requirement{{
		Action:   "pull.edit",
		Resource: authz.PullRef(o.repo, o.number),
		Context:  map[string]string{"change_hash": contentHash(parts...)},
	}}
}

// Describe returns a human summary of the requested changes for the approval page.
//
// @return string A sentence listing the edits to be applied.
//
// @testcase TestPullEditDescribe checks the repo, number and a change appear.
func (o *PullEditOperation) Describe() string {
	var changes []string
	if o.titleSet {
		changes = append(changes, fmt.Sprintf("set title to %q", o.title))
	}
	if o.bodySet {
		changes = append(changes, "set body")
	}
	if o.baseSet {
		changes = append(changes, fmt.Sprintf("retarget base to %q", o.base))
	}
	return fmt.Sprintf("Edit pull request #%d of GitHub repository %s: %s", o.number, o.repo, strings.Join(changes, "; "))
}

// Execute applies the edits via the GitHub REST API and returns the updated pull
// request object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw updated pull request object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestPullEditExecutePatches edits title and base against a stub API.
func (o *PullEditOperation) Execute(ctx context.Context) (map[string]any, error) {
	payload := map[string]any{}
	if o.titleSet {
		payload["title"] = o.title
	}
	if o.bodySet {
		payload["body"] = o.body
	}
	if o.baseSet {
		payload["base"] = o.base
	}

	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d", apiBaseURL, o.repo, o.number)
	var updated map[string]any
	if err := patchJSON(ctx, o.token, endpoint, payload, &updated); err != nil {
		return nil, fmt.Errorf("edit pull request: %w", err)
	}
	return updated, nil
}
