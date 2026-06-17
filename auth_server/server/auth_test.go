package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// enabledAuth builds an enabled authenticator with a fixed session secret.
//
// @return *Authenticator An enabled authenticator for tests.
//
// @testcase TestSessionRoundTrip builds an authenticator with this helper.
func enabledAuth() *Authenticator {
	return NewAuthenticator(AuthConfig{ClientID: "id", ClientSecret: "sec", SessionSecret: []byte("k"), BaseURL: "http://as.example"})
}

// TestAuthenticatorDisabledWhenUnconfigured reports disabled without credentials and
// enabled with them.
func TestAuthenticatorDisabledWhenUnconfigured(t *testing.T) {
	if NewAuthenticator(AuthConfig{}).Enabled() {
		t.Fatal("authenticator with no credentials should be disabled")
	}
	if !enabledAuth().Enabled() {
		t.Fatal("authenticator with credentials should be enabled")
	}
}

// TestRequireRedirectsAnonymous redirects an unauthenticated request to the login.
func TestRequireRedirectsAnonymous(t *testing.T) {
	h := enabledAuth().Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/proposal/1", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, loginPath) {
		t.Fatalf("Location = %q, want it to start with %q", loc, loginPath)
	}
}

// TestRequirePassesThroughWhenDisabled serves the wrapped handler when auth is off.
func TestRequirePassesThroughWhenDisabled(t *testing.T) {
	served := false
	h := NewAuthenticator(AuthConfig{}).Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !served {
		t.Fatal("disabled authenticator should pass through")
	}
}

// TestSessionRoundTrip stores an email in a session cookie and reads it back.
func TestSessionRoundTrip(t *testing.T) {
	a := enabledAuth()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(a.sessionCookieFor("me@example.com"))
	email, ok := a.CurrentEmail(r)
	if !ok || email != "me@example.com" {
		t.Fatalf("CurrentEmail = %q,%v; want me@example.com,true", email, ok)
	}
}

// TestLoginRedirectsToGitHub redirects to GitHub with a state cookie set.
func TestLoginRedirectsToGitHub(t *testing.T) {
	a := enabledAuth()
	rec := httptest.NewRecorder()
	a.handleLogin(rec, httptest.NewRequest(http.MethodGet, loginPath+"?next=/proposal/1", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, defaultGitHubAuthorizeURL) || !strings.Contains(loc, "state=") {
		t.Fatalf("unexpected Location: %q", loc)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("no state cookie set")
	}
}

// TestCallbackRejectsBadState rejects a callback whose state does not match the cookie.
func TestCallbackRejectsBadState(t *testing.T) {
	a := enabledAuth()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, callbackPath+"?state=wrong&code=x", nil)
	req.AddCookie(a.cookie(stateCookie, "right|/", 0))
	a.handleCallback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestLogoutClearsSession expires the session cookie and redirects home.
func TestLogoutClearsSession(t *testing.T) {
	a := enabledAuth()
	rec := httptest.NewRecorder()
	a.handleLogout(rec, httptest.NewRequest(http.MethodPost, logoutPath, nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("session cookie was not cleared")
	}
}

// TestSafeNext accepts local paths and rejects absolute or protocol-relative URLs.
func TestSafeNext(t *testing.T) {
	if safeNext("/proposal/1") != "/proposal/1" {
		t.Fatal("local path should be preserved")
	}
	for _, bad := range []string{"//evil.com", "https://evil.com", ""} {
		if safeNext(bad) != "/" {
			t.Fatalf("safeNext(%q) should be /", bad)
		}
	}
}
