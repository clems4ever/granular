package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clems4ever/granular/auth_server/store"
)

// newAuthServer builds a server with consent authentication enabled, returning the handler
// and the authenticator used to mint session cookies for a given approver.
//
// @arg t The test handle.
// @return http.Handler The mounted handler.
// @return *Authenticator The enabled authenticator.
//
// @testcase TestActivityPageRendersApproverHistory builds an authenticated server.
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
// @testcase TestActivityPageRendersApproverHistory fetches /activity with a session.
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

// TestActivityPageRendersApproverHistory shows the signed-in approver their own request in
// the history.
func TestActivityPageRendersApproverHistory(t *testing.T) {
	h, auth := newAuthServer(t)
	token := createSubject(t, h)
	_ = propose(t, h, token, "me@example.com")

	code, body := getWithSession(t, h, auth, "/activity", "me@example.com")
	if code != http.StatusOK {
		t.Fatalf("GET /activity = %d, want 200", code)
	}
	for _, want := range []string{"Your approvals", "View repo r", "badge-pending"} {
		if !strings.Contains(body, want) {
			t.Fatalf("/activity missing %q", want)
		}
	}
}

// TestActivityScopedToApprover hides requests addressed to a different approver: a user who
// approves nothing sees an empty page even when other approvers have requests.
func TestActivityScopedToApprover(t *testing.T) {
	h, auth := newAuthServer(t)
	token := createSubject(t, h)
	_ = propose(t, h, token, "other@example.com")

	code, body := getWithSession(t, h, auth, "/activity", "me@example.com")
	if code != http.StatusOK {
		t.Fatalf("GET /activity = %d, want 200", code)
	}
	if !strings.Contains(body, "No requests addressed to you yet") {
		t.Fatalf("/activity should be empty for a non-approver; got:\n%s", body)
	}
	if strings.Contains(body, "badge-") {
		t.Fatal("/activity leaked another approver's request")
	}
}

// TestActivityUnavailableWhenAuthDisabled returns 404 when consent authentication is off:
// there is no approver identity to scope the page to.
func TestActivityUnavailableWhenAuthDisabled(t *testing.T) {
	_, h := newServer(t)
	resp := do(t, h, http.MethodGet, "/activity", nil, "", false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /activity = %d, want 404 when auth disabled", resp.StatusCode)
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
