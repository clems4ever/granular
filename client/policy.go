package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/internal/proposal"
)

// Grant is one active grant attached to the policy token: which gateway authored it,
// when it expires, and the opaque gateway-signed item it carries.
type Grant struct {
	GatewayID string                      `json:"gateway_id"`
	ExpiresAt string                      `json:"expires_at"`
	Item      proposal.SignedGrantRequest `json:"item"`
}

// policyResult is the AS response from the /api/policy endpoints.
type policyResult struct {
	Token  string  `json:"token,omitempty"`
	Grants []Grant `json:"grants,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// Token returns the client's current policy token (empty when none is set).
//
// @return string The configured or freshly created policy token.
//
// @testcase TestCreatePolicySetsToken reads the token after creation.
func (c *Client) Token() string { return c.token }

// CreatePolicy mints a new policy on the AS and adopts its token for subsequent calls
// (proposals and policy reads). The caller should persist the returned token.
//
// @arg ctx Context for cancellation.
// @return string The new policy token.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestCreatePolicySetsToken creates a policy and adopts the token.
func (c *Client) CreatePolicy(ctx context.Context) (string, error) {
	var res policyResult
	status, err := c.doJSON(ctx, http.MethodPut, c.asURL+"/api/policy", "", nil, &res)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated || res.Token == "" {
		return "", fmt.Errorf("create policy: status %d: %s", status, res.Error)
	}
	c.token = res.Token
	return res.Token, nil
}

// Policy returns the active grants attached to the client's policy token.
//
// @arg ctx Context for cancellation.
// @return []Grant The active grants on the policy.
// @error ErrNoToken when no policy token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestPolicyReadsGrants lists the grants on a policy.
func (c *Client) Policy(ctx context.Context) ([]Grant, error) {
	if c.token == "" {
		return nil, ErrNoToken
	}
	var res policyResult
	status, err := c.doJSON(ctx, http.MethodGet, c.asURL+"/api/policy", c.token, nil, &res)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("read policy: status %d: %s", status, res.Error)
	}
	return res.Grants, nil
}

// DestroyPolicy destroys the client's policy and all grants attached to it, returning
// how many grants were removed.
//
// @arg ctx Context for cancellation.
// @return int The number of grants destroyed.
// @error ErrNoToken when no policy token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestDestroyPolicy destroys a policy via the endpoint.
func (c *Client) DestroyPolicy(ctx context.Context) (int, error) {
	if c.token == "" {
		return 0, ErrNoToken
	}
	var res struct {
		Destroyed int    `json:"destroyed"`
		Error     string `json:"error,omitempty"`
	}
	status, err := c.doJSON(ctx, http.MethodDelete, c.asURL+"/api/policy", c.token, nil, &res)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("destroy policy: status %d: %s", status, res.Error)
	}
	return res.Destroyed, nil
}
