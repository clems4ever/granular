package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/internal/proposal"
)

// Grant is one active grant attached to a policy: which gateway authored it, when it
// expires, and the opaque gateway-signed item it carries.
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

// Token returns the client's configured bearer token (empty when none is set). For a
// policy-administration client this is the admin token; for a grant client it is the
// policy token used for proposals and operations.
//
// @return string The configured bearer token.
//
// @testcase TestCreatePolicyReturnsToken checks the admin token is unchanged after use.
func (c *Client) Token() string { return c.token }

// CreatePolicy mints a new policy on the AS and returns its token. This is an
// administrative call authenticated with the client's admin token; the returned policy
// token is a separate credential the administrator hands to a grant client.
//
// @arg ctx Context for cancellation.
// @return string The new policy token.
// @error ErrNoToken when no admin token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestCreatePolicyReturnsToken creates a policy with the admin token.
func (c *Client) CreatePolicy(ctx context.Context) (string, error) {
	if c.token == "" {
		return "", ErrNoToken
	}
	var res policyResult
	status, err := c.doJSON(ctx, http.MethodPut, c.asURL+"/api/policy", c.token, nil, &res)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated || res.Token == "" {
		return "", fmt.Errorf("create policy: status %d: %s", status, res.Error)
	}
	return res.Token, nil
}

// Policy returns the active grants attached to a policy token. This is an administrative
// call authenticated with the client's admin token; policyToken names the policy to
// inspect.
//
// @arg ctx Context for cancellation.
// @arg policyToken The policy token to inspect.
// @return []Grant The active grants on the policy.
// @error ErrNoToken when no admin token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestPolicyReadsGrants lists the grants on a policy.
func (c *Client) Policy(ctx context.Context, policyToken string) ([]Grant, error) {
	if c.token == "" {
		return nil, ErrNoToken
	}
	var res policyResult
	status, err := c.doJSON(ctx, http.MethodGet, c.asURL+"/api/policy/"+policyToken, c.token, nil, &res)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("read policy: status %d: %s", status, res.Error)
	}
	return res.Grants, nil
}

// DestroyPolicy destroys a policy and all grants attached to it, returning how many
// grants were removed. This is an administrative call authenticated with the client's
// admin token; policyToken names the policy to destroy.
//
// @arg ctx Context for cancellation.
// @arg policyToken The policy token to destroy.
// @return int The number of grants destroyed.
// @error ErrNoToken when no admin token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestDestroyPolicy destroys a policy via the endpoint.
func (c *Client) DestroyPolicy(ctx context.Context, policyToken string) (int, error) {
	if c.token == "" {
		return 0, ErrNoToken
	}
	var res struct {
		Destroyed int    `json:"destroyed"`
		Error     string `json:"error,omitempty"`
	}
	status, err := c.doJSON(ctx, http.MethodDelete, c.asURL+"/api/policy/"+policyToken, c.token, nil, &res)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("destroy policy: status %d: %s", status, res.Error)
	}
	return res.Destroyed, nil
}
