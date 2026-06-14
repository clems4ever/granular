package github

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// postJSON performs an authenticated POST against the GitHub REST API with a JSON
// payload and decodes the (2xx) response body into dst.
//
// @arg ctx Context for cancellation of the call.
// @arg token The GitHub token; when empty the request is sent unauthenticated.
// @arg endpoint The fully-qualified request URL.
// @arg payload The value marshalled as the JSON request body.
// @arg dst A pointer the JSON response body is decoded into (may be nil to discard).
// @error error when the request fails, GitHub returns a non-2xx status, or the body cannot be decoded.
//
// @testcase TestIssueCommentExecutePosts posts a comment via postJSON.
// @testcase TestIssueCreateExecutePosts posts a new issue via postJSON.
func postJSON(ctx context.Context, token, endpoint string, payload, dst any) error {
	return sendJSON(ctx, http.MethodPost, token, endpoint, payload, dst)
}

// patchJSON performs an authenticated PATCH against the GitHub REST API with a
// JSON payload and decodes the (2xx) response body into dst.
//
// @arg ctx Context for cancellation of the call.
// @arg token The GitHub token; when empty the request is sent unauthenticated.
// @arg endpoint The fully-qualified request URL.
// @arg payload The value marshalled as the JSON request body.
// @arg dst A pointer the JSON response body is decoded into (may be nil to discard).
// @error error when the request fails, GitHub returns a non-2xx status, or the body cannot be decoded.
//
// @testcase TestIssueCloseExecutePatches closes an issue via patchJSON.
// @testcase TestIssueEditExecutePatches edits an issue via patchJSON.
func patchJSON(ctx context.Context, token, endpoint string, payload, dst any) error {
	return sendJSON(ctx, http.MethodPatch, token, endpoint, payload, dst)
}

// putJSON performs an authenticated PUT against the GitHub REST API with a JSON
// payload and decodes the (2xx) response body into dst.
//
// @arg ctx Context for cancellation of the call.
// @arg token The GitHub token; when empty the request is sent unauthenticated.
// @arg endpoint The fully-qualified request URL.
// @arg payload The value marshalled as the JSON request body.
// @arg dst A pointer the JSON response body is decoded into (may be nil to discard).
// @error error when the request fails, GitHub returns a non-2xx status, or the body cannot be decoded.
//
// @testcase TestPullMergeExecutePuts merges a pull request via putJSON.
func putJSON(ctx context.Context, token, endpoint string, payload, dst any) error {
	return sendJSON(ctx, http.MethodPut, token, endpoint, payload, dst)
}

// getRaw performs an authenticated GET against the GitHub REST API requesting the
// given media type (e.g. "application/vnd.github.diff") and returns the response
// body verbatim, without JSON decoding.
//
// @arg ctx Context for cancellation of the call.
// @arg token The GitHub token; when empty the request is sent unauthenticated.
// @arg endpoint The fully-qualified request URL.
// @arg accept The Accept media type to request.
// @return string The response body as received from GitHub.
// @error error when the request cannot be built, the call fails, or GitHub returns a non-200 status.
//
// @testcase TestPullDiffExecuteReturnsRaw fetches a unified diff via getRaw.
func getRaw(ctx context.Context, token, endpoint, accept string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := apiClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github returned %d: %s", resp.StatusCode, string(body[:min(len(body), 2048)]))
	}
	return string(body), nil
}

// sendJSON marshals payload, sends it with the given method to the GitHub REST
// API, and decodes the (2xx) response body into dst.
//
// @arg ctx Context for cancellation of the call.
// @arg method The HTTP method (POST or PATCH).
// @arg token The GitHub token; when empty the request is sent unauthenticated.
// @arg endpoint The fully-qualified request URL.
// @arg payload The value marshalled as the JSON request body.
// @arg dst A pointer the JSON response body is decoded into (may be nil to discard).
// @error error when the request cannot be built, the call fails, GitHub returns a non-2xx status, or the body cannot be decoded.
//
// @testcase TestIssueCommentExecutePosts drives sendJSON via postJSON.
func sendJSON(ctx context.Context, method, token, endpoint string, payload, dst any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := apiClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("github returned %d: %s", resp.StatusCode, string(body))
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// contentHash returns a short, stable hash of the given content parts, used to
// scope a write grant to exactly the content being submitted (so changing the
// text requires a fresh approval).
//
// @arg parts The content strings to hash together.
// @return string The first 12 hex characters of the SHA-256 of the parts.
//
// @testcase TestIssueCommentPermissionKeyIsContentScoped relies on the hash differing per body.
func contentHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
