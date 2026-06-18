package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clems4ever/granular/authserver/store"
)

// newAuthServer builds a server with consent authentication enabled, returning the handler
// and the authenticator used to mint session cookies for a given approver.
//
// @arg t The test handle.
// @return http.Handler The mounted handler.
// @return *Authenticator The enabled authenticator.
//
// @testcase TestHomeShowsApproverHistory builds an authenticated server.
func newAuthServer(t *testing.T) (http.Handler, *Authenticator) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "as.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(st, "http://as.example", map[string]string{"rs": rsSecret})
	srv.UseAdminToken(adminToken)
	auth := NewAuthenticator(AuthConfig{ClientID: "id", ClientSecret: "sec", SessionSecret: []byte("k"), BaseURL: "http://as.example"})
	srv.UseAuth(auth)
	return srv.Handler(), auth
}

// getWithSession issues GET path against h with a session cookie for email (none when
// email is ""), returning the status code and the response body.
//
// @arg t The test handle.
// @arg h The handler.
// @arg auth The authenticator minting the session cookie.
// @arg path The request path.
// @arg email The signed-in approver email, or "" for an anonymous request.
// @return int The response status code.
// @return string The response body.
//
// @testcase TestHomeShowsApproverHistory fetches / with a session.
func getWithSession(t *testing.T, h http.Handler, auth *Authenticator, path, email string) (int, string) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if email != "" {
		req.AddCookie(auth.sessionCookieFor(email))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// TestHomeShowsApproverHistory shows the signed-in approver their own request in
// the history.
func TestHomeShowsApproverHistory(t *testing.T) {
	h, auth := newAuthServer(t)
	token := createSubject(t, h)
	_ = propose(t, h, token, "me@example.com")

	code, body := getWithSession(t, h, auth, "/", "me@example.com")
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	for _, want := range []string{"Your approvals", "View repo r", "badge-pending"} {
		if !strings.Contains(body, want) {
			t.Fatalf("home page missing %q", want)
		}
	}
}

// TestHomeScopedToApprover hides requests addressed to a different approver: a signed-in
// user who approves nothing sees an empty history even when other approvers have requests.
func TestHomeScopedToApprover(t *testing.T) {
	h, auth := newAuthServer(t)
	token := createSubject(t, h)
	_ = propose(t, h, token, "other@example.com")

	code, body := getWithSession(t, h, auth, "/", "me@example.com")
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if !strings.Contains(body, "No requests addressed to you yet") {
		t.Fatalf("home should be empty for a non-approver; got:\n%s", body)
	}
	if strings.Contains(body, "badge-") {
		t.Fatal("home leaked another approver's request")
	}
}

// TestHomeShowsSignInWhenAnonymous offers a GitHub login to an anonymous visitor when
// consent authentication is enabled, instead of any per-user data.
func TestHomeShowsSignInWhenAnonymous(t *testing.T) {
	h, auth := newAuthServer(t)
	code, body := getWithSession(t, h, auth, "/", "") // no session
	if code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", code)
	}
	if !strings.Contains(body, "Sign in with GitHub") || !strings.Contains(body, "/auth/github/login") {
		t.Fatalf("home page missing sign-in call to action:\n%s", body)
	}
	if strings.Contains(body, "Your approvals") {
		t.Fatal("home leaked approver data to an anonymous visitor")
	}
}

// TestMySubjectReturnsOwnGrants returns the bearer subject's own grants via
// GET /api/subject/me, without the subject token echoed back.
func TestMySubjectReturnsOwnGrants(t *testing.T) {
	_, h := newServer(t)
	token := createSubject(t, h)
	id := propose(t, h, token, "me@example.com")
	approve(t, h, id)

	resp := do(t, h, http.MethodGet, "/api/subject/me", nil, token, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/subject/me = %d, want 200", resp.StatusCode)
	}
	var out subjectOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Grants) != 1 || out.Grants[0].ResourceServerID != "rs" {
		t.Fatalf("unexpected grants: %+v", out.Grants)
	}
	if out.Grants[0].SubjectToken != "" {
		t.Fatalf("self view should not echo the subject token; got %q", out.Grants[0].SubjectToken)
	}
}

// TestMySubjectRejectsUnknownToken rejects a missing or unknown bearer token.
func TestMySubjectRejectsUnknownToken(t *testing.T) {
	_, h := newServer(t)
	for _, tok := range []string{"", "bogus"} {
		resp := do(t, h, http.MethodGet, "/api/subject/me", nil, tok, false)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET /api/subject/me bearer=%q = %d, want 401", tok, resp.StatusCode)
		}
	}
}

// TestRevokeMyGrantsRevokesOwnGrants revokes the bearer subject's own grants via
// DELETE /api/subject/me/grants and leaves the subject token usable.
func TestRevokeMyGrantsRevokesOwnGrants(t *testing.T) {
	_, h := newServer(t)
	token := createSubject(t, h)
	approve(t, h, propose(t, h, token, "me@example.com"))

	resp := do(t, h, http.MethodDelete, "/api/subject/me/grants", nil, token, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/subject/me/grants = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Revoked int `json:"revoked"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Revoked != 1 {
		t.Fatalf("revoked = %d, want 1", out.Revoked)
	}
	// The subject survives and now holds no grants.
	r2 := do(t, h, http.MethodGet, "/api/subject/me", nil, token, false)
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/subject/me after revoke = %d, want 200 (subject should survive)", r2.StatusCode)
	}
	var read subjectOutput
	_ = json.NewDecoder(r2.Body).Decode(&read)
	if len(read.Grants) != 0 {
		t.Fatalf("want 0 grants after revoke, got %d", len(read.Grants))
	}
}

// TestRevokeMyGrantsRejectsUnknownToken rejects a missing or unknown bearer token.
func TestRevokeMyGrantsRejectsUnknownToken(t *testing.T) {
	_, h := newServer(t)
	for _, tok := range []string{"", "bogus"} {
		resp := do(t, h, http.MethodDelete, "/api/subject/me/grants", nil, tok, false)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("DELETE /api/subject/me/grants bearer=%q = %d, want 401", tok, resp.StatusCode)
		}
	}
}

// TestActivityAdminRequiresAdminToken rejects the operator view without the admin token.
func TestActivityAdminRequiresAdminToken(t *testing.T) {
	_, h := newServer(t)
	for _, tok := range []string{"", "wrong"} {
		resp := do(t, h, http.MethodGet, "/api/activity", nil, tok, false)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("GET /api/activity bearer=%q = %d, want 401", tok, resp.StatusCode)
		}
	}
}

// TestActivityAdminReturnsInventory returns the cross-subject grant inventory and history,
// with each grant and history row carrying its subject token.
func TestActivityAdminReturnsInventory(t *testing.T) {
	_, h := newServer(t)
	token := createSubject(t, h)
	id := propose(t, h, token, "me@example.com")
	approve(t, h, id)

	resp := do(t, h, http.MethodGet, "/api/activity", nil, adminToken, false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/activity = %d, want 200", resp.StatusCode)
	}
	var out activityOutput
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if len(out.Grants) != 1 || out.Grants[0].SubjectToken != token {
		t.Fatalf("unexpected grants: %+v", out.Grants)
	}
	if len(out.History) != 1 || out.History[0].Approver != "me@example.com" || out.History[0].SubjectToken != token {
		t.Fatalf("unexpected history: %+v", out.History)
	}
}
