package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TypeIssueList is the operation type id for listing a repository's issues.
const TypeIssueList = "github.issue.list"

// defaultIssueLimit is how many issues are listed when no limit is requested.
const defaultIssueLimit = 30

// IssueListOperation lists the issues of a GitHub repository server-side using the
// server-held PAT, returning them to the client.
type IssueListOperation struct {
	repo  string
	state string
	limit int
	token string
}

// IssueList builds an IssueListOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" (required),
// "state" (optional: open|closed|all, default open), "limit" (optional).
//
// @arg params The wire parameters carrying repo, state and limit.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueListOperation.
// @error ErrMissingRepo if the "repo" parameter is absent or empty.
//
// @testcase TestIssueListFactoryRequiresRepo fails when repo is missing.
// @testcase TestIssueListRequirements builds and checks the key.
func IssueList(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	state := stringParam(params, "state")
	if state == "" {
		state = "open"
	}
	return &IssueListOperation{
		repo:  NormalizeRepo(repo),
		state: state,
		limit: intParam(params, "limit", defaultIssueLimit),
		token: env.GitHubToken,
	}, nil
}

// Type returns the github.issue.list type id.
//
// @return string The constant TypeIssueList.
//
// @testcase TestIssueListRequirements exercises a built operation.
func (o *IssueListOperation) Type() string { return TypeIssueList }

// Requirements authorizes listing the repository's issues (a repo-scoped read; the
// state is a query filter, not part of the grant scope).
//
// @return []authz.Requirement A single issue.list requirement on the repository.
//
// @testcase TestIssueListRequirements checks the action and resource.
func (o *IssueListOperation) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "issue.list", Resource: authz.RepoRef(o.repo)}}
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the listing to be approved.
//
// @testcase TestIssueListDescribe checks the repo and state appear in the summary.
func (o *IssueListOperation) Describe() string {
	return fmt.Sprintf("List %s issues of GitHub repository %s", o.state, o.repo)
}

// Execute calls the GitHub REST API to list the repository's issues and returns
// GitHub's response verbatim (every attribute, every item) under "issues". Note
// the GitHub issues endpoint also includes pull requests.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result with "issues" set to GitHub's raw issue array.
// @error error when the request cannot be built, the call fails, or GitHub returns non-200.
//
// @testcase TestIssueListExecuteReturnsRaw lists issues against a stub API.
func (o *IssueListOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues?state=%s&per_page=%s",
		apiBaseURL, o.repo, url.QueryEscape(o.state), strconv.Itoa(o.limit))

	var issues []any
	if err := getJSON(ctx, o.token, endpoint, &issues); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return map[string]any{"issues": issues}, nil
}

// intParam reads an integer-valued parameter, accepting JSON numbers (float64) or
// numeric strings, returning fallback when absent or unparseable.
//
// @arg params The parameter map from the wire request.
// @arg key The parameter name to read.
// @arg fallback The value to return when the key is missing or invalid.
// @return int The parsed integer, or fallback.
//
// @testcase TestIntParam checks number, string and fallback cases.
func intParam(params map[string]any, key string, fallback int) int {
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// boolParam reads a boolean-valued parameter, returning false when absent or not a
// bool.
//
// @arg params The parameter map from the wire request.
// @arg key The parameter name to read.
// @return bool The boolean value, or false when missing or non-bool.
//
// @testcase TestIssueViewCommentsAddsRequirement relies on the comments bool param.
func boolParam(params map[string]any, key string) bool {
	b, _ := params[key].(bool)
	return b
}

// stringsParam reads a string-slice parameter, accepting either a []string or a
// JSON-decoded []any of strings, and returns the contained strings.
//
// @arg params The parameter map from the wire request.
// @arg key The parameter name to read.
// @return []string The string values, or an empty slice when absent.
//
// @testcase TestIssueCreateRequirementsAreContentScoped uses labels via this helper.
func stringsParam(params map[string]any, key string) []string {
	switch v := params[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
