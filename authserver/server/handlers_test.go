package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clems4ever/granular/internal/proposal"
)

// propose submits a one-item signed proposal for token and returns the proposal id.
//
// @arg t The test handle.
// @arg h The handler.
// @arg token The subject token.
// @arg email The approver email.
// @return string The created proposal id.
//
// @testcase TestGetSubjectReturnsGrants proposes through here.
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
// @testcase TestGetSubjectReturnsGrants approves through here.
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

// TestGetSubjectReturnsGrants returns the attached grants once a proposal is approved.
func TestGetSubjectReturnsGrants(t *testing.T) {
	_, h := newServer(t)
	token := createSubject(t, h)
	id := propose(t, h, token, "me@example.com")
	approve(t, h, id)

	resp := do(t, h, http.MethodGet, "/api/subject/"+token, nil, adminToken, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/subject/{token} = %d, want 200", resp.StatusCode)
	}
	var out subjectOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Grants) != 1 || out.Grants[0].ResourceServerID != "rs" {
		t.Fatalf("unexpected grants: %+v", out.Grants)
	}
}

// TestDestroySubjectEndpoint destroys a subject and then no longer finds it.
func TestDestroySubjectEndpoint(t *testing.T) {
	_, h := newServer(t)
	token := createSubject(t, h)

	resp := do(t, h, http.MethodDelete, "/api/subject/"+token, nil, adminToken, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/subject/{token} = %d, want 200", resp.StatusCode)
	}
	// The token no longer identifies a subject.
	resp2 := do(t, h, http.MethodGet, "/api/subject/"+token, nil, adminToken, false)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after destroy = %d, want 404", resp2.StatusCode)
	}
}

// TestSubjectAdminRequiresAdminToken checks the subject-administration endpoints reject a
// missing or wrong admin token, and are disabled when no admin token is configured.
func TestSubjectAdminRequiresAdminToken(t *testing.T) {
	srv, h := newServer(t)

	// No token / wrong token are rejected.
	for _, bearer := range []string{"", "wrong"} {
		resp := do(t, h, http.MethodPut, "/api/subject", nil, bearer, false)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("PUT with bearer %q = %d, want 401", bearer, resp.StatusCode)
		}
	}

	// With no admin token configured the endpoints are disabled.
	srv.UseAdminToken("")
	resp := do(t, h, http.MethodPut, "/api/subject", nil, adminToken, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("PUT with admin disabled = %d, want 503", resp.StatusCode)
	}
}

// TestApprovePageRendersFriendlyGrants checks the consent card leads with a single
// heading (the summary), lists each permission as a friendly phrase on its resource, and
// shows the attribute conditions in plain language.
func TestApprovePageRendersFriendlyGrants(t *testing.T) {
	_, h := newServer(t)
	token := createSubject(t, h)
	item := proposal.Sign([]byte(rsSecret), "rs", proposal.Presentation{
		Summary: "Read everything in clems4ever/granular",
		Grants: []proposal.GrantDetail{{
			Actions:      []string{"Everything readable: list/view, clone, read comments."},
			ResourceType: "Repository",
			Resource:     "clems4ever/granular",
			Conditions:   []string{"state is open"},
		}},
	}, []string{`permit(principal, action, resource);`})
	pin, _ := json.Marshal(proposalInput{ApproverEmail: "me@example.com", Items: []proposal.SignedGrantRequest{item}})
	resp := do(t, h, http.MethodPost, "/api/proposals", pin, token, false)
	var out proposalOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	page := do(t, h, http.MethodGet, "/proposal/"+out.ProposalID, nil, "", false)
	defer page.Body.Close()
	body, _ := io.ReadAll(page.Body)
	html := string(body)
	for _, want := range []string{
		"request-summary",                        // single heading class (no double title)
		"Read everything in clems4ever/granular", // the summary heading text
		"Everything readable",                    // friendly permission phrase, not "read"
		`class="restype">Repository`,             // the human resource type label
		"clems4ever/granular",                    // resource value
		"but only when",                          // conditions label
		"state is open",                          // the condition phrase
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("consent page missing %q", want)
		}
	}
}

// TestProposalExpiresViaEndpoint checks a proposal with a short request TTL is shown as
// expired on the consent page and can no longer be approved through the endpoint.
func TestProposalExpiresViaEndpoint(t *testing.T) {
	srv, h := newServer(t)
	srv.UseRequestTTL(time.Millisecond)
	token := createSubject(t, h)
	id := propose(t, h, token, "me@example.com")
	time.Sleep(15 * time.Millisecond) // let the request lapse

	// The consent page reflects the expired status (no approve form).
	page := do(t, h, http.MethodGet, "/proposal/"+id, nil, "", false)
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(string(body), "badge-expired") {
		t.Fatalf("consent page does not show expired status:\n%s", string(body))
	}

	// Approving the lapsed request is refused with a clear message.
	ts := httptest.NewServer(h)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/proposal/"+id, strings.NewReader("decision=approve&ttl=1h"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(rb), "expired") {
		t.Fatalf("approve of lapsed request not reported as expired:\n%s", string(rb))
	}

	// And no grant attached.
	g := do(t, h, http.MethodGet, "/api/subject/"+token, nil, adminToken, false)
	defer g.Body.Close()
	var out subjectOutput
	_ = json.NewDecoder(g.Body).Decode(&out)
	if len(out.Grants) != 0 {
		t.Fatalf("expired request attached %d grant(s)", len(out.Grants))
	}
}

// TestProposalRejectsUnknownSubjectToken rejects a proposal whose bearer token does not
// identify a persisted subject (empty or never created), so a grant cannot attach to a
// non-existent subject.
func TestProposalRejectsUnknownSubjectToken(t *testing.T) {
	_, h := newServer(t)
	pin, _ := json.Marshal(proposalInput{ApproverEmail: "me@example.com", Items: []proposal.SignedGrantRequest{signedItem()}})
	for _, token := range []string{"", "never-created"} {
		resp := do(t, h, http.MethodPost, "/api/proposals", pin, token, false)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("propose with token %q = %d, want 401", token, resp.StatusCode)
		}
	}
}

// TestProposalRequiresApproverEmail rejects a proposal with no approver email.
func TestProposalRequiresApproverEmail(t *testing.T) {
	_, h := newServer(t)
	token := createSubject(t, h)
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
	token := createSubject(t, h)
	id := propose(t, h, token, "me@example.com")
	if verifyAllowed(t, h, token) {
		t.Fatal("allowed before approval")
	}
	approve(t, h, id)
	if !verifyAllowed(t, h, token) {
		t.Fatal("denied after approval; want allowed")
	}
}

// TestHomeLandingWhenAuthDisabled renders the informational landing at / when consent
// authentication is disabled (there is no approver identity to scope activity to).
func TestHomeLandingWhenAuthDisabled(t *testing.T) {
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
	if strings.Contains(string(body), "Your approvals") || strings.Contains(string(body), "Sign in with GitHub") {
		t.Fatal("auth-disabled landing should show neither activity nor a sign-in prompt")
	}
}
