package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/clems4ever/granular/internal/operations"
)

// TypeIssueList is the operation type id for listing a repository's issues.
const TypeIssueList = "github.issue.list"

// defaultIssueLimit is how many issues are listed when no limit is requested.
const defaultIssueLimit = 30

// apiClient is the HTTP client used for GitHub REST API calls.
var apiClient = &http.Client{Timeout: 30 * time.Second}

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
// @testcase TestIssueListPermissionKeyIncludesState builds and checks the key.
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
// @testcase TestIssueListPermissionKeyIncludesState exercises a built operation.
func (o *IssueListOperation) Type() string { return TypeIssueList }

// PermissionKey returns a grant key scoped to the repository and issue state, so
// approving "list open issues" does not authorise listing closed ones.
//
// @return string A key of the form "github.issue.list:<owner/name>?state=<state>".
//
// @testcase TestIssueListPermissionKeyIncludesState checks the key shape.
func (o *IssueListOperation) PermissionKey() string {
	return fmt.Sprintf("%s:%s?state=%s", TypeIssueList, o.repo, o.state)
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the listing to be approved.
//
// @testcase TestIssueListDescribe checks the repo and state appear in the summary.
func (o *IssueListOperation) Describe() string {
	return fmt.Sprintf("List %s issues of GitHub repository %s", o.state, o.repo)
}

// Execute calls the GitHub REST API to list the repository's issues (excluding
// pull requests) and returns them as structured result entries.
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result with an "issues" slice of {number,title,state,author,url}.
// @error error when the request cannot be built, the call fails, or GitHub returns non-200.
//
// @testcase TestIssueListExecuteParsesAndFiltersPRs lists issues against a stub API.
func (o *IssueListOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues?state=%s&per_page=%s",
		apiBaseURL, o.repo, url.QueryEscape(o.state), strconv.Itoa(o.limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if o.token != "" {
		req.Header.Set("Authorization", "Bearer "+o.token)
	}

	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("list issues: github returned %d: %s", resp.StatusCode, string(body))
	}

	var raw []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		PullRequest *struct{} `json:"pull_request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode issues: %w", err)
	}

	issues := make([]map[string]any, 0, len(raw))
	for _, it := range raw {
		if it.PullRequest != nil {
			continue // the issues endpoint also returns PRs; exclude them
		}
		issues = append(issues, map[string]any{
			"number": it.Number,
			"title":  it.Title,
			"state":  it.State,
			"author": it.User.Login,
			"url":    it.HTMLURL,
		})
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

// apiBaseURL is the GitHub REST API base; overridable in tests.
var apiBaseURL = "https://api.github.com"
