// Package client is the HTTP client the granular CLI uses to talk to the granular
// server: it submits operations and polls delegation-request status.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/clems4ever/granular/internal/api"
)

// Client talks to a granular server over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a Client targeting the given server base URL.
//
// @arg baseURL The server base URL, e.g. "http://localhost:8080".
// @return *Client A client with a default timeout.
//
// @testcase TestSubmitDecodesResponse constructs a client against a test server.
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 5 * time.Minute}}
}

// Submit posts an operation attempt and returns the server's response.
//
// @arg ctx Context for cancellation.
// @arg req The operation type and parameters to attempt.
// @return api.OperationResponse The decoded server response (pending or completed).
// @error error on transport failure, non-2xx status, or undecodable body.
//
// @testcase TestSubmitDecodesResponse submits and checks the decoded response.
func (c *Client) Submit(ctx context.Context, req api.OperationRequest) (api.OperationResponse, error) {
	return c.post(ctx, "/api/operations", req)
}

// RequestPermissions posts a custom permissions request and returns the server's
// response (pending, with an approval URL).
//
// @arg ctx Context for cancellation.
// @arg req The capabilities to request.
// @return api.OperationResponse The decoded server response.
// @error error on transport failure, non-2xx status, or undecodable body.
//
// @testcase TestRequestPermissionsDecodesResponse requests and checks the response.
func (c *Client) RequestPermissions(ctx context.Context, req api.PermissionsRequest) (api.OperationResponse, error) {
	return c.post(ctx, "/api/permissions", req)
}

// post marshals payload, POSTs it to the path, and decodes the OperationResponse.
//
// @arg ctx Context for cancellation.
// @arg path The server path to POST to.
// @arg payload The value to marshal as the JSON body.
// @return api.OperationResponse The decoded server response.
// @error error on transport failure, a 5xx status, or undecodable body.
//
// @testcase TestSubmitDecodesResponse drives post via Submit.
func (c *Client) post(ctx context.Context, path string, payload any) (api.OperationResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return api.OperationResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return api.OperationResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return api.OperationResponse{}, err
	}
	defer resp.Body.Close()

	var out api.OperationResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return api.OperationResponse{}, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 500 {
		return out, fmt.Errorf("server error (status %d): %s", resp.StatusCode, out.Error)
	}
	return out, nil
}

// Catalog fetches the raw capability manifest JSON from the server.
//
// @arg ctx Context for cancellation.
// @return []byte The raw /api/catalog response body.
// @error error on transport failure or a non-2xx status.
//
// @testcase TestCatalogFetchesManifest fetches and checks the body.
func (c *Client) Catalog(ctx context.Context) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/catalog", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog request failed: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
