package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypeIssueEdit is the operation type id for editing an issue's fields (not its
// open/closed status, which is github.issue.close / github.issue.reopen).
const TypeIssueEdit = "github.issue.edit"

// IssueEditOperation edits a GitHub issue's fields server-side. Label and assignee
// changes are expressed as add/remove sets applied to the issue's current values.
// This is a mutating operation.
type IssueEditOperation struct {
	repo            string
	number          int
	title           string
	titleSet        bool
	body            string
	bodySet         bool
	addLabels       []string
	removeLabels    []string
	addAssignees    []string
	removeAssignees []string
	token           string
}

// IssueEdit builds an IssueEditOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" and "number"
// (required); optional "title", "body", "add_labels", "remove_labels",
// "add_assignees", "remove_assignees". At least one change is required.
//
// @arg params The wire parameters carrying repo, number and the field changes.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueEditOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingIssueNumber if "number" is absent or not positive.
// @error ErrNoChanges if no field change is requested.
//
// @testcase TestIssueEditFactoryValidatesParams fails on bad params or no changes.
// @testcase TestIssueEditRequirementsAreContentScoped builds and checks the key.
func IssueEdit(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingIssueNumber
	}

	title, titleSet := params["title"].(string)
	body, bodySet := params["body"].(string)
	op := &IssueEditOperation{
		repo:            NormalizeRepo(repo),
		number:          number,
		title:           title,
		titleSet:        titleSet,
		body:            body,
		bodySet:         bodySet,
		addLabels:       stringsParam(params, "add_labels"),
		removeLabels:    stringsParam(params, "remove_labels"),
		addAssignees:    stringsParam(params, "add_assignees"),
		removeAssignees: stringsParam(params, "remove_assignees"),
		token:           env.GitHubToken,
	}
	if !op.hasChanges() {
		return nil, ErrNoChanges
	}
	return op, nil
}

// hasChanges reports whether the operation requests any field change.
//
// @return bool True when at least one field is being edited.
//
// @testcase TestIssueEditFactoryValidatesParams checks the no-change case.
func (o *IssueEditOperation) hasChanges() bool {
	return o.titleSet || o.bodySet ||
		len(o.addLabels) > 0 || len(o.removeLabels) > 0 ||
		len(o.addAssignees) > 0 || len(o.removeAssignees) > 0
}

// Type returns the github.issue.edit type id.
//
// @return string The constant TypeIssueEdit.
//
// @testcase TestIssueEditRequirementsAreContentScoped exercises a built operation.
func (o *IssueEditOperation) Type() string { return TypeIssueEdit }

// Requirements authorizes editing the issue, qualified by a hash of the exact set
// of changes, so approving one edit does not authorise a different one.
//
// @return []authz.Requirement A single issue.edit requirement, context-scoped to the change set.
//
// @testcase TestIssueEditRequirementsAreContentScoped checks the change-set hash context.
func (o *IssueEditOperation) Requirements() []authz.Requirement {
	var parts []string
	if o.titleSet {
		parts = append(parts, "title="+o.title)
	}
	if o.bodySet {
		parts = append(parts, "body="+o.body)
	}
	parts = append(parts,
		"addL="+strings.Join(o.addLabels, ","),
		"rmL="+strings.Join(o.removeLabels, ","),
		"addA="+strings.Join(o.addAssignees, ","),
		"rmA="+strings.Join(o.removeAssignees, ","),
	)
	return []authz.Requirement{{
		Action:   "issue.edit",
		Resource: authz.IssueRef(o.repo, o.number),
		Context:  map[string]string{"change_hash": contentHash(parts...)},
	}}
}

// Describe returns a human summary of the requested changes for the approval page.
//
// @return string A sentence listing the edits to be applied.
//
// @testcase TestIssueEditDescribe checks the repo, number and a change appear.
func (o *IssueEditOperation) Describe() string {
	var changes []string
	if o.titleSet {
		changes = append(changes, fmt.Sprintf("set title to %q", o.title))
	}
	if o.bodySet {
		changes = append(changes, "set body")
	}
	if len(o.addLabels) > 0 {
		changes = append(changes, "add labels ["+strings.Join(o.addLabels, ", ")+"]")
	}
	if len(o.removeLabels) > 0 {
		changes = append(changes, "remove labels ["+strings.Join(o.removeLabels, ", ")+"]")
	}
	if len(o.addAssignees) > 0 {
		changes = append(changes, "add assignees ["+strings.Join(o.addAssignees, ", ")+"]")
	}
	if len(o.removeAssignees) > 0 {
		changes = append(changes, "remove assignees ["+strings.Join(o.removeAssignees, ", ")+"]")
	}
	return fmt.Sprintf("Edit issue #%d of GitHub repository %s: %s", o.number, o.repo, strings.Join(changes, "; "))
}

// Execute applies the edits via the GitHub REST API and returns the updated issue
// object verbatim. Label and assignee changes are merged against the issue's
// current values, which requires reading the issue first.
//
// @arg ctx Context for cancellation of the API calls.
// @return map[string]any Result set to GitHub's raw updated-issue object.
// @error error when a request fails or GitHub returns a non-2xx status.
//
// @testcase TestIssueEditExecutePatches edits title, labels and assignees against a stub API.
func (o *IssueEditOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d", apiBaseURL, o.repo, o.number)

	payload := map[string]any{}
	if o.titleSet {
		payload["title"] = o.title
	}
	if o.bodySet {
		payload["body"] = o.body
	}

	editsLabels := len(o.addLabels) > 0 || len(o.removeLabels) > 0
	editsAssignees := len(o.addAssignees) > 0 || len(o.removeAssignees) > 0
	if editsLabels || editsAssignees {
		var current map[string]any
		if err := getJSON(ctx, o.token, endpoint, &current); err != nil {
			return nil, fmt.Errorf("read issue for edit: %w", err)
		}
		if editsLabels {
			payload["labels"] = applyAddRemove(namesFrom(current, "labels", "name"), o.addLabels, o.removeLabels)
		}
		if editsAssignees {
			payload["assignees"] = applyAddRemove(namesFrom(current, "assignees", "login"), o.addAssignees, o.removeAssignees)
		}
	}

	var updated map[string]any
	if err := patchJSON(ctx, o.token, endpoint, payload, &updated); err != nil {
		return nil, fmt.Errorf("edit issue: %w", err)
	}
	return updated, nil
}

// namesFrom extracts the string field (e.g. "name" or "login") from each object in
// a raw issue's array attribute (e.g. "labels" or "assignees").
//
// @arg issue The raw GitHub issue object.
// @arg arrayKey The issue attribute holding an array of objects.
// @arg field The string field to read from each object.
// @return []string The extracted values, in order.
//
// @testcase TestIssueEditExecutePatches relies on current labels/assignees extraction.
func namesFrom(issue map[string]any, arrayKey, field string) []string {
	arr, _ := issue[arrayKey].([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if m, ok := e.(map[string]any); ok {
			if s, ok := m[field].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// applyAddRemove returns current with the remove set deleted and the add set
// appended, preserving order and de-duplicating.
//
// @arg current The current values.
// @arg add Values to add (if not already present).
// @arg remove Values to remove.
// @return []string The resulting set.
//
// @testcase TestApplyAddRemove checks add, remove and dedup behaviour.
func applyAddRemove(current, add, remove []string) []string {
	removeSet := make(map[string]bool, len(remove))
	for _, r := range remove {
		removeSet[r] = true
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(current)+len(add))
	for _, c := range current {
		if removeSet[c] || seen[c] {
			continue
		}
		seen[c] = true
		result = append(result, c)
	}
	for _, a := range add {
		if seen[a] {
			continue
		}
		seen[a] = true
		result = append(result, a)
	}
	return result
}

// ErrNoChanges is returned by IssueEdit when no field change is requested.
var ErrNoChanges = fmt.Errorf("no changes requested")
