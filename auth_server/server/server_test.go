package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clems4ever/granular/auth_server/store"
	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/internal/verify"
)

const gwSecret = "s3cret"

// The Cedar policy and matching world used by the verify tests: agent "a" may view
// repo "r".
const testPolicy = `permit(principal == Granular::Agent::"a", action == Granular::Action::"view", resource == Granular::Repo::"r");`

// newServer returns a server registered with gateway "gw"/gwSecret and its handler.
//
// @arg t The test handle.
// @return *Server The server under test.
// @return http.Handler Its mounted handler.
//
// @testcase TestProposalApproveFlow builds a server with this helper.
func newServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "as.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(st, "http://as.example", map[string]string{"gw": gwSecret})
	return srv, srv.Handler()
}

// do sends a request to h and returns the response, optionally with a bearer token
// and a gateway HMAC signature over the body.
//
// @arg t The test handle.
// @arg h The handler.
// @arg method The HTTP method.
// @arg path The request path.
// @arg body The request body (may be nil).
// @arg bearer A policy token for the Authorization header, or "".
// @arg sign Whether to attach a valid gateway signature over the body.
// @return *http.Response The response.
//
// @testcase TestProposalApproveFlow drives requests through here.
func do(t *testing.T, h http.Handler, method, path string, body []byte, bearer string, sign bool) *http.Response {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if sign {
		mac := hmac.New(sha256.New, []byte(gwSecret))
		mac.Write(body)
		req.Header.Set("X-Gateway-ID", "gw")
		req.Header.Set("X-Gateway-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

// createPolicy PUTs a new policy and returns its token.
//
// @arg t The test handle.
// @arg h The handler.
// @return string The new policy token.
//
// @testcase TestProposalApproveFlow mints a token through here.
func createPolicy(t *testing.T, h http.Handler) string {
	t.Helper()
	resp := do(t, h, http.MethodPut, "/api/policy", nil, "", false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT /api/policy = %d, want 201", resp.StatusCode)
	}
	var out policyOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Token == "" {
		t.Fatal("no token returned")
	}
	return out.Token
}

// signedItem builds a gateway-signed grant request carrying testPolicy.
//
// @return proposal.SignedGrantRequest A valid signed item for gateway "gw".
//
// @testcase TestProposalApproveFlow proposes this item.
func signedItem() proposal.SignedGrantRequest {
	return proposal.Sign([]byte(gwSecret), "gw",
		proposal.Presentation{Title: "View", Summary: "View repo r"},
		[]string{testPolicy})
}

// verifyBody builds the JSON verify body for the agent/view/repo request.
//
// @arg token The policy token to evaluate against.
// @return []byte The marshalled verifyInput.
//
// @testcase TestProposalApproveFlow verifies through here.
func verifyBody(token string) []byte {
	b, _ := json.Marshal(verify.Input{
		Token: token,
		Requests: []verify.Request{{
			Principal: verify.EntityRef{Type: "Granular::Agent", ID: "a"},
			Action:    verify.EntityRef{Type: "Granular::Action", ID: "view"},
			Resource:  verify.EntityRef{Type: "Granular::Repo", ID: "r"},
		}},
		Entities: []verify.Entity{
			{Type: "Granular::Agent", ID: "a"},
			{Type: "Granular::Action", ID: "view"},
			{Type: "Granular::Repo", ID: "r"},
		},
	})
	return b
}

// verifyAllowed posts a signed verify request and returns the allowed flag.
//
// @arg t The test handle.
// @arg h The handler.
// @arg token The policy token.
// @return bool The decision.
//
// @testcase TestProposalApproveFlow reads the decision through here.
func verifyAllowed(t *testing.T, h http.Handler, token string) bool {
	t.Helper()
	resp := do(t, h, http.MethodPost, "/api/verify", verifyBody(token), "", true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("verify = %d, want 200", resp.StatusCode)
	}
	var out verify.Output
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Allowed
}

// TestProposalApproveFlow drives the whole path: mint a policy token, submit a signed
// proposal, approve it via the (auth-disabled) consent endpoint, and verify the
// operation is then allowed.
func TestProposalApproveFlow(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)

	pin, _ := json.Marshal(proposalInput{ApproverEmail: "me@example.com", Items: []proposal.SignedGrantRequest{signedItem()}})
	resp := do(t, h, http.MethodPost, "/api/proposals", pin, token, false)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /api/proposals = %d, want 202", resp.StatusCode)
	}
	var pout proposalOutput
	_ = json.NewDecoder(resp.Body).Decode(&pout)
	resp.Body.Close()
	if pout.ProposalID == "" {
		t.Fatal("no proposal id")
	}

	if verifyAllowed(t, h, token) {
		t.Fatal("allowed before approval")
	}

	// Approve through the consent endpoint (auth disabled => approverGate passes).
	form := strings.NewReader("decision=approve&ttl=1h")
	ts := httptest.NewServer(h)
	defer ts.Close()
	areq, _ := http.NewRequest(http.MethodPost, ts.URL+"/proposal/"+pout.ProposalID, form)
	areq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	aresp, err := http.DefaultClient.Do(areq)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	aresp.Body.Close()
	if aresp.StatusCode != http.StatusOK {
		t.Fatalf("approve = %d, want 200", aresp.StatusCode)
	}

	if !verifyAllowed(t, h, token) {
		t.Fatal("denied after approval; want allowed")
	}
}

// TestProposalRejectsBadSignature rejects a proposal whose item is signed with the
// wrong secret (a tampering/forging client).
func TestProposalRejectsBadSignature(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)
	bad := proposal.Sign([]byte("wrong"), "gw", proposal.Presentation{Summary: "x"}, []string{testPolicy})
	pin, _ := json.Marshal(proposalInput{ApproverEmail: "me@example.com", Items: []proposal.SignedGrantRequest{bad}})
	resp := do(t, h, http.MethodPost, "/api/proposals", pin, token, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestPolicyRejectsUnknownToken rejects bearer access with an unregistered token.
func TestPolicyRejectsUnknownToken(t *testing.T) {
	_, h := newServer(t)
	resp := do(t, h, http.MethodGet, "/api/policy", nil, "nope", false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestVerifyRejectsUnknownGateway rejects a verify call not signed by a known gateway.
func TestVerifyRejectsUnknownGateway(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)
	resp := do(t, h, http.MethodPost, "/api/verify", verifyBody(token), "", false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
