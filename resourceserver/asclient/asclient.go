// Package asclient is the resource server's HTTP client to the authorization server (AS). It
// calls POST /api/verify, signing the request body with the resource server's shared HMAC
// secret so the AS can authenticate it.
package asclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/clems4ever/granular/internal/verify"
)

// Client calls the AS verify endpoint on behalf of the resource server.
type Client struct {
	baseURL          string
	resourceServerID string
	secret           []byte
	httpClient       *http.Client
}

// New creates a Client.
//
// @arg baseURL The AS base URL (e.g. http://localhost:9090).
// @arg resourceServerID The resource server's registered id, sent in X-Resource-Server-ID.
// @arg secret The resource server's shared HMAC secret.
// @return *Client A ready client.
//
// @testcase TestVerifySignsBody constructs a client.
func New(baseURL, resourceServerID string, secret []byte) *Client {
	return &Client{
		baseURL:          strings.TrimRight(baseURL, "/"),
		resourceServerID: resourceServerID,
		secret:           secret,
		httpClient:       &http.Client{Timeout: 15 * time.Second},
	}
}

// Verify asks the AS whether the subject identified by in.Token authorizes the requests.
// It signs the JSON body with the resource server secret (X-Resource-Server-ID + X-Resource-Server-Signature).
//
// @arg ctx Context for cancellation.
// @arg in The verify input (token, requests, entity world).
// @return bool True when the AS allows every request.
// @error error on transport failure, a non-2xx status, or a decode error.
//
// @testcase TestVerifySignsBody sends a correctly-signed body the AS accepts.
func (c *Client) Verify(ctx context.Context, in verify.Input) (bool, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/verify", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Resource-Server-ID", c.resourceServerID)
	req.Header.Set("X-Resource-Server-Signature", c.sign(body))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("verify endpoint returned %d", resp.StatusCode)
	}
	var out verify.Output
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	if out.Error != "" {
		return false, fmt.Errorf("verify error: %s", out.Error)
	}
	return out.Allowed, nil
}

// sign returns the hex HMAC-SHA256 of body under the resource server secret.
//
// @arg body The request body to authenticate.
// @return string The hex-encoded HMAC.
//
// @testcase TestVerifySignsBody relies on a valid signature.
func (c *Client) sign(body []byte) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
