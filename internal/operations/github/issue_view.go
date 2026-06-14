package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/clems4ever/granular/internal/operations"
)

// TypeIssueView is the operation type id for viewing a single issue's details.
const TypeIssueView = "github.issue.view"

// IssueViewOperation fetches the details of a single GitHub issue server-side
// using the server-held PAT.
type IssueViewOperation struct {
	repo   string
	number int
	token  string
}

// IssueView builds an IssueViewOperation from request parameters and the server
// Env. It satisfies operations.Factory. Expected params: "repo" (required) and
// "number" (required, the issue number).
//
// @arg params The wire parameters carrying repo and number.
// @arg env The server Env supplying the GitHub token.
// @return operations.Operation A ready-to-execute IssueViewOperation.
// @error ErrMissingRepo if "repo" is absent or empty.
// @error ErrMissingIssueNumber if "number" is absent or not positive.
//
// @testcase TestIssueViewFactoryValidatesParams fails on missing repo or number.
// @testcase TestIssueViewPermissionKeyIncludesNumber builds and checks the key.
func IssueView(params map[string]any, env operations.Env) (operations.Operation, error) {
	repo := stringParam(params, "repo")
	if repo == "" {
		return nil, ErrMissingRepo
	}
	number := intParam(params, "number", 0)
	if number <= 0 {
		return nil, ErrMissingIssueNumber
	}
	return &IssueViewOperation{
		repo:   NormalizeRepo(repo),
		number: number,
		token:  env.GitHubToken,
	}, nil
}

// Type returns the github.issue.view type id.
//
// @return string The constant TypeIssueView.
//
// @testcase TestIssueViewPermissionKeyIncludesNumber exercises a built operation.
func (o *IssueViewOperation) Type() string { return TypeIssueView }

// PermissionKey returns a grant key scoped to the specific repository and issue
// number, so approving one issue does not authorise viewing another.
//
// @return string A key of the form "github.issue.view:<owner/name>#<number>".
//
// @testcase TestIssueViewPermissionKeyIncludesNumber checks the key shape.
func (o *IssueViewOperation) PermissionKey() string {
	return fmt.Sprintf("%s:%s#%d", TypeIssueView, o.repo, o.number)
}

// Describe returns a one-line human summary for the approval page.
//
// @return string A sentence describing the issue to be viewed.
//
// @testcase TestIssueViewDescribe checks the repo and number appear in the summary.
func (o *IssueViewOperation) Describe() string {
	return fmt.Sprintf("View issue #%d of GitHub repository %s", o.number, o.repo)
}

// Execute calls the GitHub REST API to fetch the issue and returns GitHub's
// response object verbatim (every attribute).
//
// @arg ctx Context for cancellation of the API call.
// @return map[string]any Result set to GitHub's raw issue object.
// @error error when the request fails or GitHub returns a non-200 status.
//
// @testcase TestIssueViewExecuteReturnsRaw fetches an issue against a stub API.
func (o *IssueViewOperation) Execute(ctx context.Context) (map[string]any, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d", apiBaseURL, o.repo, o.number)

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
		return nil, fmt.Errorf("view issue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("view issue: github returned %d: %s", resp.StatusCode, string(body))
	}

	var issue map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("decode issue: %w", err)
	}
	return issue, nil
}

// ErrMissingIssueNumber is returned by IssueView when no positive issue number is
// supplied.
var ErrMissingIssueNumber = fmt.Errorf("missing or invalid required parameter: number")
