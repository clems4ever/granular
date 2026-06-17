package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/internal/proposal"
)

// Grant is one active grant attached to a subject: which resource server authored it, when it
// expires, and the opaque resource server-signed item it carries.
type Grant struct {
	ResourceServerID string                      `json:"resource_server_id"`
	ExpiresAt        string                      `json:"expires_at"`
	Item             proposal.SignedGrantRequest `json:"item"`
}

// subjectResult is the AS response from the /api/subject endpoints.
type subjectResult struct {
	Token  string  `json:"token,omitempty"`
	Grants []Grant `json:"grants,omitempty"`
	Error  string  `json:"error,omitempty"`
}

// Token returns the client's configured bearer token (empty when none is set). For a
// subject-administration client this is the admin token; for a grant client it is the
// subject token used for proposals and operations.
//
// @return string The configured bearer token.
//
// @testcase TestCreateSubjectReturnsToken checks the admin token is unchanged after use.
func (c *Client) Token() string { return c.token }

// CreateSubject mints a new subject on the AS and returns its token. This is an
// administrative call authenticated with the client's admin token; the returned subject
// token is a separate credential the administrator hands to a grant client.
//
// @arg ctx Context for cancellation.
// @return string The new subject token.
// @error ErrNoToken when no admin token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestCreateSubjectReturnsToken creates a subject with the admin token.
func (c *Client) CreateSubject(ctx context.Context) (string, error) {
	if c.token == "" {
		return "", ErrNoToken
	}
	var res subjectResult
	status, err := c.doJSON(ctx, http.MethodPut, c.asURL+"/api/subject", c.token, nil, &res)
	if err != nil {
		return "", err
	}
	if status != http.StatusCreated || res.Token == "" {
		return "", fmt.Errorf("create subject: status %d: %s", status, res.Error)
	}
	return res.Token, nil
}

// Subject returns the active grants attached to a subject token. This is an administrative
// call authenticated with the client's admin token; subjectToken names the subject to
// inspect.
//
// @arg ctx Context for cancellation.
// @arg subjectToken The subject token to inspect.
// @return []Grant The active grants on the subject.
// @error ErrNoToken when no admin token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestSubjectReadsGrants lists the grants on a subject.
func (c *Client) Subject(ctx context.Context, subjectToken string) ([]Grant, error) {
	if c.token == "" {
		return nil, ErrNoToken
	}
	var res subjectResult
	status, err := c.doJSON(ctx, http.MethodGet, c.asURL+"/api/subject/"+subjectToken, c.token, nil, &res)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("read subject: status %d: %s", status, res.Error)
	}
	return res.Grants, nil
}

// DestroySubject destroys a subject and all grants attached to it, returning how many
// grants were removed. This is an administrative call authenticated with the client's
// admin token; subjectToken names the subject to destroy.
//
// @arg ctx Context for cancellation.
// @arg subjectToken The subject token to destroy.
// @return int The number of grants destroyed.
// @error ErrNoToken when no admin token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestDestroySubject destroys a subject via the endpoint.
func (c *Client) DestroySubject(ctx context.Context, subjectToken string) (int, error) {
	if c.token == "" {
		return 0, ErrNoToken
	}
	var res struct {
		Destroyed int    `json:"destroyed"`
		Error     string `json:"error,omitempty"`
	}
	status, err := c.doJSON(ctx, http.MethodDelete, c.asURL+"/api/subject/"+subjectToken, c.token, nil, &res)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("destroy subject: status %d: %s", status, res.Error)
	}
	return res.Destroyed, nil
}
