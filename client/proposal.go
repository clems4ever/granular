package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/resourceserver"
)

// Proposal is the AS's answer to a submitted proposal: its id, the URL the approver
// visits to review and decide, and when the pending request expires.
type Proposal struct {
	ID        string
	URL       string
	ExpiresAt string
}

// proposalSubmit is the body posted to the AS POST /api/proposals endpoint.
type proposalSubmit struct {
	ApproverEmail string                        `json:"approver_email"`
	Reason        string                        `json:"reason,omitempty"`
	Items         []proposal.SignedGrantRequest `json:"items"`
}

// proposalResult is the AS response from POST /api/proposals.
type proposalResult struct {
	ProposalID string `json:"proposal_id"`
	URL        string `json:"url"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Sign asks one resource server to sign a capability bundle, returning the resource server-signed grant
// request. The resource server validates the actions against its own schema and authors the
// human-readable description, so the caller can store the result and later submit it
// (alone or bundled with other resource servers' signed requests) without being able to tamper
// with it.
//
// @arg ctx Context for cancellation.
// @arg resourceServerID The resource server that signs (and validates) the bundle.
// @arg req The capability bundle to sign.
// @return proposal.SignedGrantRequest The resource server-signed grant request.
// @error ErrUnknownResourceServer when the resource server id is not configured.
// @error error when the resource server rejects the capabilities or signing fails.
//
// @testcase TestSignReturnsSignedRequest signs a bundle at a resource server.
// @testcase TestSignUnknownResourceServer errors on an unconfigured resource server.
func (c *Client) Sign(ctx context.Context, resourceServerID string, req resourceserver.GrantRequest) (proposal.SignedGrantRequest, error) {
	var signed proposal.SignedGrantRequest
	base, err := c.resourceServerURL(resourceServerID)
	if err != nil {
		return signed, err
	}
	status, err := c.doJSON(ctx, http.MethodPost, base+"/api/grant-requests/sign", "", req, &signed)
	if err != nil {
		return signed, err
	}
	if status != http.StatusOK {
		return signed, fmt.Errorf("resource server %q rejected the grant request (status %d)", resourceServerID, status)
	}
	return signed, nil
}

// Submit packs one or more resource server-signed grant requests into a proposal and submits it
// to the AS under the client's subject token, returning the proposal id and the approval
// URL to hand to the user. The signed items may come from different resource servers; the AS
// verifies each one's signature independently. The optional reason is unsigned context shown
// to the approver explaining why the grants are needed.
//
// @arg ctx Context for cancellation.
// @arg approverEmail The email of the human who must approve.
// @arg reason The optional, unsigned context explaining why the grants are needed.
// @arg items The resource server-signed grant requests to bundle.
// @return Proposal The submitted proposal's id and approval URL.
// @error ErrNoToken when no subject token is configured.
// @error error when the approver/items are missing or the AS rejects the proposal.
//
// @testcase TestSubmitSendsBundle submits a signed bundle to the AS.
func (c *Client) Submit(ctx context.Context, approverEmail, reason string, items []proposal.SignedGrantRequest) (Proposal, error) {
	var out Proposal
	if c.token == "" {
		return out, ErrNoToken
	}
	if approverEmail == "" {
		return out, fmt.Errorf("approver email is required")
	}
	if len(items) == 0 {
		return out, fmt.Errorf("no signed grant requests to submit")
	}
	var res proposalResult
	status, err := c.doJSON(ctx, http.MethodPost, c.asURL+"/api/proposals", c.token, proposalSubmit{
		ApproverEmail: approverEmail,
		Reason:        reason,
		Items:         items,
	}, &res)
	if err != nil {
		return out, err
	}
	if status != http.StatusAccepted {
		return out, fmt.Errorf("submit proposal: status %d: %s", status, res.Error)
	}
	return Proposal{ID: res.ProposalID, URL: res.URL, ExpiresAt: res.ExpiresAt}, nil
}
