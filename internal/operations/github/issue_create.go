package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/clems4ever/granular/internal/operations"
)

// TypeIssueCreate is the operation type id for creating an issue.
const TypeIssueCreate = "github.issue.create"

// IssueCreateOperation creates an issue on a GitHub repository server-side using
// the server-held PAT. This is a mutating operation.
type IssueCreateOperation struct {
	repo      string
	title     string
	body      string
	labels    []string
	assignees []string
	token     string
}

// IssueCreate builds an IssueCreateOperation from request parameters and the
// server Env. It satisfies operations.Factory. Expected params: "repo" and
// "title" (required), "body", "labels", "assignees" (optional).
//
// @arg params The wire parameters carrying repo, title, body, labels and assignees.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueCreateOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingTitle if "title" is absent or empty.
//
// @testcase TestIssueCreateFactoryValidatesParams fails on missing repo or title.
// @testcase TestIssueCreatePermissionKeyIsContentScoped builds and checks the key.
func IssueCreate(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	title := stringParam(params, "title")
	if title == "" {
		return nil, ErrMissingTitle
	}
	return &IssueCreateOperation{
		repo:      NormalizeRepo(repo),
		title:     title,
		body:      stringParam(params, "body"),
		labels:    stringsParam(params, "labels"),
		assignees: stringsParam(params, "assignees"),
		token:     env.GitHubToken,
	}, nil
}

// Type returns the github.issue.create type id.
//
// @return string The constant TypeIssueCreate.
//
// @testcase TestIssueCreatePermissionKeyIsContentScoped exercises a built operation.
func (o *IssueCreateOperation) Type() string { return TypeIssueCreate }

// PermissionKey returns a grant key scoped to the repository and the exact issue
// content (title, body, labels, assignees), so approving one issue does not
// authorise creating a different one.
//
// @return string A key of the form "github.issue.create:<owner/name>:<hash>".
//
// @testcase TestIssueCreatePermissionKeyIsContentScoped checks the key changes with content.
func (o *IssueCreateOperation) PermissionKey() string {
	parts := append([]string{o.title, o.body}, o.labels...)
	parts = append(parts, o.assignees...)
	return fmt.Sprintf("%s:%s:%s", TypeIssueCreate, o.repo, contentHash(parts...))
}

// Describe returns a human summary for the approval page, including the title and
// body so the approver sees what will be created.
//
// @return string A sentence describing the issue to be created.
//
// @testcase TestIssueCreateDescribe checks the repo and title appear.
func (o *IssueCreateOperation) Describe() string {
	summary := fmt.Sprintf("Create an issue in GitHub repository %s titled %q", o.repo, o.title)
	if len(o.labels) > 0 {
		summary += fmt.Sprintf(" with labels [%s]", strings.Join(o.labels, ", "))
	}
	if o.body != "" {
		summary += fmt.Sprintf(":\n\n%s", o.body)
	}
	return summary
}

// Execute creates the issue via the GitHub REST API and returns GitHub's created
// issue object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw created-issue object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestIssueCreateExecutePosts creates an issue against a stub API.
func (o *IssueCreateOperation) Execute(ctx context.Context) (map[string]any, error) {
	payload := map[string]any{"title": o.title}
	if o.body != "" {
		payload["body"] = o.body
	}
	if len(o.labels) > 0 {
		payload["labels"] = o.labels
	}
	if len(o.assignees) > 0 {
		payload["assignees"] = o.assignees
	}

	endpoint := fmt.Sprintf("%s/repos/%s/issues", apiBaseURL, o.repo)
	var created map[string]any
	if err := postJSON(ctx, o.token, endpoint, payload, &created); err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}
	return created, nil
}

// ErrMissingTitle is returned by IssueCreate when no title is supplied.
var ErrMissingTitle = fmt.Errorf("missing required parameter: title")
