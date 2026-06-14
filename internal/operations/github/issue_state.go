package github

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/internal/operations"
)

// TypeIssueClose and TypeIssueReopen are the operation type ids for changing an
// issue's open/closed status. They are deliberately separate from issue.edit so a
// grant to change status cannot also edit the issue's content.
const (
	TypeIssueClose  = "github.issue.close"
	TypeIssueReopen = "github.issue.reopen"
)

// IssueCloseOperation closes a GitHub issue server-side, optionally recording a
// state reason. This is a mutating operation.
type IssueCloseOperation struct {
	repo   string
	number int
	reason string
	token  string
}

// IssueClose builds an IssueCloseOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" and "number"
// (required) and "reason" (optional: "completed" or "not planned").
//
// @arg params The wire parameters carrying repo, number and reason.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueCloseOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingIssueNumber if "number" is absent or not positive.
// @error ErrInvalidCloseReason if "reason" is set to an unsupported value.
//
// @testcase TestIssueCloseFactoryValidatesParams fails on bad params or reason.
// @testcase TestIssueClosePermissionKey builds and checks the key.
func IssueClose(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingIssueNumber
	}
	reason, ok := normalizeCloseReason(stringParam(params, "reason"))
	if !ok {
		return nil, ErrInvalidCloseReason
	}
	return &IssueCloseOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		reason: reason,
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.issue.close type id.
//
// @return string The constant TypeIssueClose.
//
// @testcase TestIssueClosePermissionKey exercises a built operation.
func (o *IssueCloseOperation) Type() string { return TypeIssueClose }

// PermissionKey returns a grant key scoped to the issue and, when set, the state
// reason.
//
// @return string A key of the form "github.issue.close:<owner/name>#<number>" (plus ":<reason>").
//
// @testcase TestIssueClosePermissionKey checks the key shape.
func (o *IssueCloseOperation) PermissionKey() string {
	key := fmt.Sprintf("%s:%s#%d", TypeIssueClose, o.repo, o.number)
	if o.reason != "" {
		key += ":" + o.reason
	}
	return key
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the close to be approved.
//
// @testcase TestIssueCloseDescribe checks the repo and number appear.
func (o *IssueCloseOperation) Describe() string {
	if o.reason != "" {
		return fmt.Sprintf("Close issue #%d of GitHub repository %s as %q", o.number, o.repo, o.reason)
	}
	return fmt.Sprintf("Close issue #%d of GitHub repository %s", o.number, o.repo)
}

// Execute closes the issue via the GitHub REST API and returns the updated issue
// object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw updated-issue object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestIssueCloseExecutePatches closes an issue against a stub API.
func (o *IssueCloseOperation) Execute(ctx context.Context) (map[string]any, error) {
	payload := map[string]any{"state": "closed"}
	if o.reason != "" {
		payload["state_reason"] = o.reason
	}
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d", apiBaseURL, o.repo, o.number)
	var updated map[string]any
	if err := patchJSON(ctx, o.token, endpoint, payload, &updated); err != nil {
		return nil, fmt.Errorf("close issue: %w", err)
	}
	return updated, nil
}

// IssueReopenOperation reopens a closed GitHub issue server-side. This is a
// mutating operation.
type IssueReopenOperation struct {
	repo   string
	number int
	token  string
}

// IssueReopen builds an IssueReopenOperation from request parameters and the
// server Env. It satisfies operations.Factory. Expected params: "repo" and
// "number" (required).
//
// @arg params The wire parameters carrying repo and number.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueReopenOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingIssueNumber if "number" is absent or not positive.
//
// @testcase TestIssueReopenFactoryValidatesParams fails on missing repo or number.
// @testcase TestIssueReopenPermissionKey builds and checks the key.
func IssueReopen(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingIssueNumber
	}
	return &IssueReopenOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.issue.reopen type id.
//
// @return string The constant TypeIssueReopen.
//
// @testcase TestIssueReopenPermissionKey exercises a built operation.
func (o *IssueReopenOperation) Type() string { return TypeIssueReopen }

// PermissionKey returns a grant key scoped to the issue.
//
// @return string A key of the form "github.issue.reopen:<owner/name>#<number>".
//
// @testcase TestIssueReopenPermissionKey checks the key shape.
func (o *IssueReopenOperation) PermissionKey() string {
	return fmt.Sprintf("%s:%s#%d", TypeIssueReopen, o.repo, o.number)
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the reopen to be approved.
//
// @testcase TestIssueReopenDescribe checks the repo and number appear.
func (o *IssueReopenOperation) Describe() string {
	return fmt.Sprintf("Reopen issue #%d of GitHub repository %s", o.number, o.repo)
}

// Execute reopens the issue via the GitHub REST API and returns the updated issue
// object verbatim.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw updated-issue object.
// @error error when the request fails or GitHub returns a non-2xx status.
//
// @testcase TestIssueReopenExecutePatches reopens an issue against a stub API.
func (o *IssueReopenOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d", apiBaseURL, o.repo, o.number)
	var updated map[string]any
	if err := patchJSON(ctx, o.token, endpoint, map[string]any{"state": "open"}, &updated); err != nil {
		return nil, fmt.Errorf("reopen issue: %w", err)
	}
	return updated, nil
}

// normalizeCloseReason maps a user-supplied close reason to the GitHub
// state_reason value, reporting whether it is valid.
//
// @arg raw The raw reason from the request ("", "completed", "not planned", …).
// @return string The GitHub state_reason value ("" when no reason given).
// @return bool True when the reason is empty or recognised.
//
// @testcase TestIssueCloseFactoryValidatesParams exercises valid and invalid reasons.
func normalizeCloseReason(raw string) (string, bool) {
	switch raw {
	case "":
		return "", true
	case "completed":
		return "completed", true
	case "not planned", "not_planned":
		return "not_planned", true
	default:
		return "", false
	}
}

// ErrInvalidCloseReason is returned when a close reason is neither "completed" nor
// "not planned".
var ErrInvalidCloseReason = fmt.Errorf(`invalid close reason (want "completed" or "not planned")`)
