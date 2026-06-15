package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testAuth builds an enabled authenticator with a fixed secret and allowlist.
func testAuth(allowed ...string) *Authenticator {
	return NewAuthenticator(AuthConfig{
		ClientID:      "cid",
		ClientSecret:  "secret",
		AllowedUsers:  allowed,
		SessionSecret: []byte("test-secret-key-0123456789abcdef"),
		BaseURL:       "http://example.test",
	})
}

// cookieNamed returns the named cookie from a response, or nil.
func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// githubStub serves the OAuth token and user endpoints for the given login.
func githubStub(t *testing.T, login string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok","token_type":"bearer"}`)
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"login":"`+login+`"}`)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// TestAuthenticatorDisabledWhenUnconfigured checks an authenticator without credentials is disabled.
func TestAuthenticatorDisabledWhenUnconfigured(t *testing.T) {
	a := NewAuthenticator(AuthConfig{BaseURL: "http://example.test"})
	if a.Enabled() {
		t.Fatal("authenticator without client id/secret should be disabled")
	}
}

// TestIsAllowed checks the allowlist is matched case-insensitively and counted.
func TestIsAllowed(t *testing.T) {
	a := testAuth("Octocat", " alice ")
	if a.AllowedCount() != 2 {
		t.Fatalf("AllowedCount = %d, want 2", a.AllowedCount())
	}
	if !a.isAllowed("octocat") || !a.isAllowed("ALICE") {
		t.Fatal("listed users should be allowed (case-insensitively)")
	}
	if a.isAllowed("mallory") {
		t.Fatal("unlisted user should not be allowed")
	}
}

// TestSafeNext checks safeNext accepts local paths and rejects absolute URLs.
func TestSafeNext(t *testing.T) {
	if got := safeNext("/grants"); got != "/grants" {
		t.Fatalf("safeNext(/grants) = %q", got)
	}
	for _, bad := range []string{"https://evil.test", "//evil.test", "", "evil"} {
		if got := safeNext(bad); got != "/" {
			t.Fatalf("safeNext(%q) = %q, want /", bad, got)
		}
	}
}

// TestRequireRedirectsAnonymous checks a protected request with no session redirects to login.
func TestRequireRedirectsAnonymous(t *testing.T) {
	a := testAuth("octocat")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/grants", nil)
	rec := httptest.NewRecorder()
	a.Require(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/auth/login?next=") {
		t.Fatalf("unexpected redirect %q", loc)
	}
}

// TestRequireAllowsValidSession checks a valid session for an allowed user reaches next.
func TestRequireAllowsValidSession(t *testing.T) {
	a := testAuth("octocat")
	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served = true })
	req := httptest.NewRequest(http.MethodGet, "/grants", nil)
	req.AddCookie(a.sessionCookieFor("octocat"))
	a.Require(next).ServeHTTP(httptest.NewRecorder(), req)
	if !served {
		t.Fatal("valid session should reach the protected handler")
	}
}

// TestRequirePassesThroughWhenDisabled checks a disabled authenticator serves next directly.
func TestRequirePassesThroughWhenDisabled(t *testing.T) {
	a := NewAuthenticator(AuthConfig{BaseURL: "http://example.test"}) // disabled
	served := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served = true })
	a.Require(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/grants", nil))
	if !served {
		t.Fatal("disabled auth should pass through to the handler")
	}
}

// TestLoginRedirectsToGitHub checks handleLogin redirects to GitHub and sets a state cookie.
func TestLoginRedirectsToGitHub(t *testing.T) {
	a := testAuth("octocat")
	req := httptest.NewRequest(http.MethodGet, "/auth/login?next=/grants", nil)
	rec := httptest.NewRecorder()
	a.handleLogin(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, a.authorizeURL) || !strings.Contains(loc, "client_id=cid") || !strings.Contains(loc, "scope=read%3Auser") {
		t.Fatalf("unexpected authorize URL %q", loc)
	}
	state := cookieNamed(rec.Result(), stateCookie)
	if state == nil || !strings.HasSuffix(state.Value, "|/grants") {
		t.Fatalf("state cookie missing or malformed: %+v", state)
	}
}

// TestCallbackSetsSessionForAllowedUser checks a successful callback sets a usable session.
func TestCallbackSetsSessionForAllowedUser(t *testing.T) {
	gh := githubStub(t, "octocat")
	a := testAuth("octocat")
	a.tokenURL, a.userURL = gh.URL+"/token", gh.URL+"/user"

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state=s1", nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: "s1|/grants"})
	rec := httptest.NewRecorder()
	a.handleCallback(rec, req)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/grants" {
		t.Fatalf("want 302 to /grants, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	sess := cookieNamed(rec.Result(), sessionCookie)
	if sess == nil {
		t.Fatal("session cookie not set")
	}
	// The session must be accepted by sessionUser.
	probe := httptest.NewRequest(http.MethodGet, "/grants", nil)
	probe.AddCookie(sess)
	if user, ok := a.sessionUser(probe); !ok || user != "octocat" {
		t.Fatalf("sessionUser = %q, %v", user, ok)
	}
}

// TestCallbackDeniesDisallowedUser checks a user outside the allowlist gets a 403 denied page.
func TestCallbackDeniesDisallowedUser(t *testing.T) {
	gh := githubStub(t, "octocat")
	a := testAuth("someone-else")
	a.tokenURL, a.userURL = gh.URL+"/token", gh.URL+"/user"

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state=s1", nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: "s1|/grants"})
	rec := httptest.NewRecorder()
	a.handleCallback(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Access denied") {
		t.Fatal("expected the denied page")
	}
	if cookieNamed(rec.Result(), sessionCookie) != nil {
		t.Fatal("no session cookie should be set for a denied user")
	}
}

// TestCallbackRejectsBadState checks a mismatched OAuth state is rejected.
func TestCallbackRejectsBadState(t *testing.T) {
	a := testAuth("octocat")
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?code=c&state=WRONG", nil)
	req.AddCookie(&http.Cookie{Name: stateCookie, Value: "s1|/grants"})
	rec := httptest.NewRecorder()
	a.handleCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for bad state, got %d", rec.Code)
	}
}

// TestLogoutClearsSession checks handleLogout expires the session cookie.
func TestLogoutClearsSession(t *testing.T) {
	a := testAuth("octocat")
	rec := httptest.NewRecorder()
	a.handleLogout(rec, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	sess := cookieNamed(rec.Result(), sessionCookie)
	if sess == nil || sess.MaxAge >= 0 {
		t.Fatalf("logout should expire the session cookie, got %+v", sess)
	}
}
