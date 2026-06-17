package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/proposal"
)

// propose submits a one-item signed proposal for token and returns the proposal id.
//
// @arg t The test handle.
// @arg h The handler.
// @arg token The policy token.
// @arg email The approver email.
// @return string The created proposal id.
//
// @testcase TestGetPolicyReturnsGrants proposes through here.
func propose(t *testing.T, h http.Handler, token, email string) string {
	t.Helper()
	pin, _ := json.Marshal(proposalInput{ApproverEmail: email, Items: []proposal.SignedGrantRequest{signedItem()}})
	resp := do(t, h, http.MethodPost, "/api/proposals", pin, token, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("propose = %d, want 202", resp.StatusCode)
	}
	var out proposalOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.ProposalID
}

// approve posts an approval decision for a proposal (auth disabled) via the consent
// endpoint.
//
// @arg t The test handle.
// @arg h The handler.
// @arg id The proposal id to approve.
//
// @testcase TestGetPolicyReturnsGrants approves through here.
func approve(t *testing.T, h http.Handler, id string) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/proposal/"+id, strings.NewReader("decision=approve&ttl=1h"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("approve = %d, want 200", resp.StatusCode)
	}
}

// TestGetPolicyReturnsGrants returns the attached grants once a proposal is approved.
func TestGetPolicyReturnsGrants(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)
	id := propose(t, h, token, "me@example.com")
	approve(t, h, id)

	resp := do(t, h, http.MethodGet, "/api/policy", nil, token, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/policy = %d, want 200", resp.StatusCode)
	}
	var out policyOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Grants) != 1 || out.Grants[0].GatewayID != "gw" {
		t.Fatalf("unexpected grants: %+v", out.Grants)
	}
}

// TestDestroyPolicyEndpoint destroys a policy and then rejects its token.
func TestDestroyPolicyEndpoint(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)

	resp := do(t, h, http.MethodDelete, "/api/policy", nil, token, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/policy = %d, want 200", resp.StatusCode)
	}
	// The token no longer identifies a policy.
	resp2 := do(t, h, http.MethodGet, "/api/policy", nil, token, false)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET after destroy = %d, want 401", resp2.StatusCode)
	}
}

// TestProposalRequiresApproverEmail rejects a proposal with no approver email.
func TestProposalRequiresApproverEmail(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)
	pin, _ := json.Marshal(proposalInput{Items: []proposal.SignedGrantRequest{signedItem()}})
	resp := do(t, h, http.MethodPost, "/api/proposals", pin, token, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestVerifyAllowsAfterApproval allows an operation once a covering grant is live.
func TestVerifyAllowsAfterApproval(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)
	id := propose(t, h, token, "me@example.com")
	if verifyAllowed(t, h, token) {
		t.Fatal("allowed before approval")
	}
	approve(t, h, id)
	if !verifyAllowed(t, h, token) {
		t.Fatal("denied after approval; want allowed")
	}
}

// TestIndexServesLanding renders the landing page.
func TestIndexServesLanding(t *testing.T) {
	_, h := newServer(t)
	resp := do(t, h, http.MethodGet, "/", nil, "", false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Authorization server") {
		t.Fatal("landing page missing expected content")
	}
}
