package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/clems4ever/granular/gateway"
	"github.com/clems4ever/granular/internal/proposal"
)

// Proposal is the AS's answer to a submitted proposal: its id and the URL the approver
// visits to review and decide.
type Proposal struct {
	ID  string
	URL string
}

// proposalSubmit is the body posted to the AS POST /api/proposals endpoint.
type proposalSubmit struct {
	ApproverEmail string                        `json:"approver_email"`
	Items         []proposal.SignedGrantRequest `json:"items"`
}

// proposalResult is the AS response from POST /api/proposals.
type proposalResult struct {
	ProposalID string `json:"proposal_id"`
	URL        string `json:"url"`
	Error      string `json:"error,omitempty"`
}

// Sign asks one gateway to sign a capability bundle, returning the gateway-signed grant
// request. The gateway validates the actions against its own schema and authors the
// human-readable description, so the caller can store the result and later submit it
// (alone or bundled with other gateways' signed requests) without being able to tamper
// with it.
//
// @arg ctx Context for cancellation.
// @arg gatewayID The gateway that signs (and validates) the bundle.
// @arg req The capability bundle to sign.
// @return proposal.SignedGrantRequest The gateway-signed grant request.
// @error ErrUnknownGateway when the gateway id is not configured.
// @error error when the gateway rejects the capabilities or signing fails.
//
// @testcase TestSignReturnsSignedRequest signs a bundle at a gateway.
// @testcase TestSignUnknownGateway errors on an unconfigured gateway.
func (c *Client) Sign(ctx context.Context, gatewayID string, req gateway.GrantRequest) (proposal.SignedGrantRequest, error) {
	var signed proposal.SignedGrantRequest
	base, err := c.gatewayURL(gatewayID)
	if err != nil {
		return signed, err
	}
	status, err := c.doJSON(ctx, http.MethodPost, base+"/api/grant-requests/sign", "", req, &signed)
	if err != nil {
		return signed, err
	}
	if status != http.StatusOK {
		return signed, fmt.Errorf("gateway %q rejected the grant request (status %d)", gatewayID, status)
	}
	return signed, nil
}

// Submit packs one or more gateway-signed grant requests into a proposal and submits it
// to the AS under the client's policy token, returning the proposal id and the approval
// URL to hand to the user. The signed items may come from different gateways; the AS
// verifies each one's signature independently.
//
// @arg ctx Context for cancellation.
// @arg approverEmail The email of the human who must approve.
// @arg items The gateway-signed grant requests to bundle.
// @return Proposal The submitted proposal's id and approval URL.
// @error ErrNoToken when no policy token is configured.
// @error error when the approver/items are missing or the AS rejects the proposal.
//
// @testcase TestSubmitSendsBundle submits a signed bundle to the AS.
func (c *Client) Submit(ctx context.Context, approverEmail string, items []proposal.SignedGrantRequest) (Proposal, error) {
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
		Items:         items,
	}, &res)
	if err != nil {
		return out, err
	}
	if status != http.StatusAccepted {
		return out, fmt.Errorf("submit proposal: status %d: %s", status, res.Error)
	}
	return Proposal{ID: res.ProposalID, URL: res.URL}, nil
}
