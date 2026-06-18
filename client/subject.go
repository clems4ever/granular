package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/internal/proposal"
)

// Grant is one active grant attached to a subject: which resource server authored it, when it
// expires, and the opaque resource server-signed item it carries. SubjectToken is set only
// in the operator activity view (which spans subjects); the per-subject reads omit it.
type Grant struct {
	SubjectToken     string                      `json:"subject_token,omitempty"`
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

// HistoryEntry is one proposal in the operator activity view: which subject it was
// submitted for, the approver it named, its decision status and a one-line summary.
type HistoryEntry struct {
	SubjectToken string `json:"subject_token"`
	Approver     string `json:"approver"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	Items        int    `json:"items"`
	CreatedAt    string `json:"created_at"`
}

// Activity is the AS operator view: the full grant inventory and the request/decision
// history across every subject.
type Activity struct {
	Grants  []Grant        `json:"grants"`
	History []HistoryEntry `json:"history"`
	Error   string         `json:"error,omitempty"`
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

// MySubject returns the active grants attached to the client's OWN subject token. Unlike
// Subject, this is not an administrative call: it authenticates with the subject token the
// client already holds (GET /api/subject/me), so a sandboxed agent can introspect what it
// currently holds without any privileged credential.
//
// @arg ctx Context for cancellation.
// @return []Grant The active grants on the caller's own subject.
// @error ErrNoToken when no subject token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestMySubjectReturnsOwnGrants lists the caller's own grants.
func (c *Client) MySubject(ctx context.Context) ([]Grant, error) {
	if c.token == "" {
		return nil, ErrNoToken
	}
	var res subjectResult
	status, err := c.doJSON(ctx, http.MethodGet, c.asURL+"/api/subject/me", c.token, nil, &res)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("read own subject: status %d: %s", status, res.Error)
	}
	return res.Grants, nil
}

// RevokeMyGrants revokes every active grant attached to the client's OWN subject token in
// one call. Like MySubject, this is not an administrative call: it authenticates with the
// subject token the client already holds (DELETE /api/subject/me/grants), so a sandboxed
// agent can drop all the authority it currently holds without any privileged credential.
// The subject token itself survives and stays usable afterward.
//
// @arg ctx Context for cancellation.
// @return int The number of grants revoked.
// @error ErrNoToken when no subject token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestRevokeMyGrantsRevokesOwnGrants revokes the caller's own grants.
// @testcase TestRevokeMyGrantsRequiresToken fails with ErrNoToken when unconfigured.
func (c *Client) RevokeMyGrants(ctx context.Context) (int, error) {
	if c.token == "" {
		return 0, ErrNoToken
	}
	var res struct {
		Revoked int    `json:"revoked"`
		Error   string `json:"error,omitempty"`
	}
	status, err := c.doJSON(ctx, http.MethodDelete, c.asURL+"/api/subject/me/grants", c.token, nil, &res)
	if err != nil {
		return 0, err
	}
	if status != http.StatusOK {
		return 0, fmt.Errorf("revoke own grants: status %d: %s", status, res.Error)
	}
	return res.Revoked, nil
}

// Activity returns the operator view: the full grant inventory and request/decision
// history across all subjects (GET /api/activity). This is an administrative call
// authenticated with the client's admin token.
//
// @arg ctx Context for cancellation.
// @return Activity The cross-subject grant inventory and history.
// @error ErrNoToken when no admin token is configured.
// @error error on transport failure or an unexpected AS status.
//
// @testcase TestActivityReturnsInventory reads the cross-subject activity.
func (c *Client) Activity(ctx context.Context) (Activity, error) {
	if c.token == "" {
		return Activity{}, ErrNoToken
	}
	var res Activity
	status, err := c.doJSON(ctx, http.MethodGet, c.asURL+"/api/activity", c.token, nil, &res)
	if err != nil {
		return Activity{}, err
	}
	if status != http.StatusOK {
		return Activity{}, fmt.Errorf("read activity: status %d: %s", status, res.Error)
	}
	return res, nil
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
