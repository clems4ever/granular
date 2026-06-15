// Package client is the HTTP client the granular CLI uses to talk to the granular
// server: it submits operations, requests capability grants, and polls
// grant-request status.
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

// SubmitOperation posts an operation to POST /api/operations: the server executes
// it when live grants authorise it (Status completed), otherwise it returns a
// pending response whose ApprovalURL a human must visit before a retry can execute.
//
// @arg ctx Context for cancellation.
// @arg op The operation type and parameters to attempt.
// @return api.RequestResponse The decoded server response (pending or completed).
// @error error on transport failure, a 5xx status, or undecodable body.
//
// @testcase TestSubmitDecodesResponse submits an operation and checks the response.
func (c *Client) SubmitOperation(ctx context.Context, op api.Operation) (api.RequestResponse, error) {
	return c.post(ctx, "/api/operations", op)
}

// RequestGrant posts a capability grant request to POST /api/grant-requests, asking
// a human to pre-approve a bundle of capabilities for later use. The response is
// always pending with an ApprovalURL; nothing is executed.
//
// @arg ctx Context for cancellation.
// @arg req The grant request carrying the capability bundle.
// @return api.RequestResponse The decoded server response (pending).
// @error error on transport failure, a 5xx status, or undecodable body.
//
// @testcase TestRequestGrantPostsToGrantRequests posts a capability bundle.
func (c *Client) RequestGrant(ctx context.Context, req api.GrantRequest) (api.RequestResponse, error) {
	return c.post(ctx, "/api/grant-requests", req)
}

// post marshals payload, POSTs it to the path, and decodes the OperationResponse.
//
// @arg ctx Context for cancellation.
// @arg path The server path to POST to.
// @arg payload The value to marshal as the JSON body.
// @return api.RequestResponse The decoded server response.
// @error error on transport failure, a 5xx status, or undecodable body.
//
// @testcase TestSubmitDecodesResponse drives post via Submit.
func (c *Client) post(ctx context.Context, path string, payload any) (api.RequestResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return api.RequestResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return api.RequestResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return api.RequestResponse{}, err
	}
	defer resp.Body.Close()

	var out api.RequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return api.RequestResponse{}, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
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

// Grants fetches the active grants and request history from the server.
//
// @arg ctx Context for cancellation.
// @return api.GrantsResponse The decoded grants and requests.
// @error error on transport failure, a non-2xx status, or an undecodable body.
//
// @testcase TestGrantsAndRevoke lists grants from a test server.
func (c *Client) Grants(ctx context.Context) (api.GrantsResponse, error) {
	var out api.GrantsResponse
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/grants", nil)
	if err != nil {
		return out, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("grants request failed: status %d", resp.StatusCode)
	}
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// Revoke asks the server to revoke the active grants for a grant id or request id.
//
// @arg ctx Context for cancellation.
// @arg id A grant id or a request id.
// @return api.RevokeResponse The decoded response with the number revoked.
// @error error on transport failure, a non-2xx status, or an undecodable body.
//
// @testcase TestGrantsAndRevoke revokes a grant via a test server.
func (c *Client) Revoke(ctx context.Context, id string) (api.RevokeResponse, error) {
	var out api.RevokeResponse
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/grants/"+id+"/revoke", nil)
	if err != nil {
		return out, err
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("revoke failed (status %d): %s", resp.StatusCode, out.Error)
	}
	return out, nil
}
