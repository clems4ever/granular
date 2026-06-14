package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// apiBaseURL is the GitHub REST API base; overridable in tests.
var apiBaseURL = "https://api.github.com"

// apiClient is the HTTP client used for GitHub REST API calls.
var apiClient = &http.Client{Timeout: 30 * time.Second}

// getJSON performs an authenticated GET against the GitHub REST API and decodes
// the JSON body into dst.
//
// @arg ctx Context for cancellation of the call.
// @arg token The GitHub token; when empty the request is sent unauthenticated.
// @arg endpoint The fully-qualified request URL.
// @arg dst A pointer the JSON body is decoded into.
// @error error when the request cannot be built, the call fails, GitHub returns a non-200 status, or the body cannot be decoded.
//
// @testcase TestIssueViewExecuteReturnsRaw fetches a single object via getJSON.
// @testcase TestIssueListExecuteReturnsRaw fetches an array via getJSON.
func getJSON(ctx context.Context, token, endpoint string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := apiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("github returned %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
