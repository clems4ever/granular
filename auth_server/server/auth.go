package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GitHub OAuth endpoints. They are fields on the Authenticator so tests can point
// them at a stub server; these are the production defaults. The login routes live
// under /auth/github/ so a second provider (Google) can be added under /auth/google/
// later without colliding.
const (
	defaultGitHubAuthorizeURL = "https://github.com/login/oauth/authorize"
	defaultGitHubTokenURL     = "https://github.com/login/oauth/access_token"
	defaultGitHubEmailsURL    = "https://api.github.com/user/emails"
)

const (
	// loginPath starts the GitHub OAuth flow; callbackPath completes it.
	loginPath    = "/auth/github/login"
	callbackPath = "/auth/github/callback"
	logoutPath   = "/auth/github/logout"

	// sessionCookie holds the signed login session (the verified email).
	sessionCookie = "granular_auth_session"
	// stateCookie holds the anti-CSRF OAuth state plus the post-login destination.
	stateCookie = "granular_auth_oauth_state"
	// sessionTTL is how long a login lasts before re-authentication.
	sessionTTL = 12 * time.Hour
)

// AuthConfig configures GitHub-OAuth protection of the consent screen.
type AuthConfig struct {
	ClientID      string
	ClientSecret  string
	SessionSecret []byte
	BaseURL       string
}

// Authenticator runs a "log in with GitHub" flow and tracks the signed-in user's
// verified primary email in a signed cookie. It performs no authorization itself:
// the consent handler compares the session email against each proposal's approver.
type Authenticator struct {
	clientID     string
	clientSecret string
	secret       []byte
	baseURL      string
	httpClient   *http.Client

	authorizeURL string
	tokenURL     string
	emailsURL    string
}

// NewAuthenticator builds an Authenticator from cfg. It is only "enabled" when both a
// client id and secret are present; otherwise the consent pages stay open and the
// caller is expected to warn. A random session secret is generated when none is set.
//
// @arg cfg The OAuth credentials, session secret and base URL.
// @return *Authenticator A configured authenticator (possibly disabled).
//
// @testcase TestAuthenticatorDisabledWhenUnconfigured builds one without credentials.
func NewAuthenticator(cfg AuthConfig) *Authenticator {
	secret := cfg.SessionSecret
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	return &Authenticator{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		secret:       secret,
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		authorizeURL: defaultGitHubAuthorizeURL,
		tokenURL:     defaultGitHubTokenURL,
		emailsURL:    defaultGitHubEmailsURL,
	}
}

// Enabled reports whether OAuth protection is configured (client id and secret set).
//
// @return bool True when the consent UI should require a GitHub login.
//
// @testcase TestAuthenticatorDisabledWhenUnconfigured checks the disabled case.
func (a *Authenticator) Enabled() bool {
	return a.clientID != "" && a.clientSecret != ""
}

// Require wraps next so that requests without a valid session are redirected into the
// GitHub login flow. When authentication is disabled it passes requests straight
// through. It does NOT authorize — the handler enforces the approver-email match.
//
// @arg next The handler to protect.
// @return http.Handler A handler that enforces a login before next.
//
// @testcase TestRequireRedirectsAnonymous redirects a request with no session.
// @testcase TestRequirePassesThroughWhenDisabled serves next when auth is off.
func (a *Authenticator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := a.sessionEmail(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, loginPath+"?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
	})
}

// CurrentEmail returns the verified email of the signed-in user, or ("", false) when
// there is no valid session (or authentication is disabled).
//
// @arg r The request whose session cookie is read.
// @return string The signed-in user's verified email.
// @return bool True when a valid session is present.
//
// @testcase TestSessionRoundTrip reads back the email it stored.
func (a *Authenticator) CurrentEmail(r *http.Request) (string, bool) {
	if !a.Enabled() {
		return "", false
	}
	return a.sessionEmail(r)
}

// handleLogin starts the GitHub OAuth flow: it stores an anti-CSRF state and the
// post-login destination in a short-lived cookie, then redirects to GitHub, asking
// for the read:user and user:email scopes (the email gates approval).
//
// @arg w The response writer.
// @arg r The incoming request; its "next" query parameter is the destination.
//
// @testcase TestLoginRedirectsToGitHub checks the redirect target and state cookie.
func (a *Authenticator) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken()
	if err != nil {
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.cookie(stateCookie, state+"|"+safeNext(r.URL.Query().Get("next")), 10*time.Minute))

	q := url.Values{}
	q.Set("client_id", a.clientID)
	q.Set("redirect_uri", a.baseURL+callbackPath)
	q.Set("scope", "read:user user:email")
	q.Set("state", state)
	q.Set("allow_signup", "false")
	http.Redirect(w, r, a.authorizeURL+"?"+q.Encode(), http.StatusFound)
}

// handleCallback completes the GitHub OAuth flow: it validates the state, exchanges
// the code for an access token, fetches the user's verified primary email, sets a
// signed session cookie, and redirects to the saved destination.
//
// @arg w The response writer.
// @arg r The callback request carrying code and state.
//
// @testcase TestCallbackRejectsBadState rejects a mismatched state.
func (a *Authenticator) handleCallback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(stateCookie)
	if err != nil {
		http.Error(w, "missing OAuth state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, a.cookie(stateCookie, "", -time.Hour)) // one-time use.

	wantState, next, _ := strings.Cut(c.Value, "|")
	if wantState == "" || r.URL.Query().Get("state") != wantState {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}

	token, err := a.exchangeCode(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "failed to exchange authorization code", http.StatusBadGateway)
		return
	}
	email, err := a.fetchEmail(r.Context(), token)
	if err != nil {
		http.Error(w, "failed to fetch a verified GitHub email", http.StatusBadGateway)
		return
	}

	http.SetCookie(w, a.sessionCookieFor(email))
	http.Redirect(w, r, safeNext(next), http.StatusFound)
}

// handleLogout clears the session cookie and redirects to the home page.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestLogoutClearsSession checks the session cookie is expired.
func (a *Authenticator) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.cookie(sessionCookie, "", -time.Hour))
	http.Redirect(w, r, "/", http.StatusFound)
}

// exchangeCode swaps an OAuth authorization code for a GitHub access token.
//
// @arg ctx Context for cancellation.
// @arg code The authorization code returned by GitHub.
// @return string The access token.
// @error error on transport failure, a non-2xx status, or a missing token.
//
// @testcase TestCallbackRejectsBadState never reaches exchange (placeholder coverage).
func (a *Authenticator) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", a.baseURL+callbackPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access token returned: %s", out.Error)
	}
	return out.AccessToken, nil
}

// fetchEmail returns the user's primary, verified GitHub email for the access token.
//
// @arg ctx Context for cancellation.
// @arg token A GitHub access token.
// @return string The primary verified email.
// @error error on transport failure, a non-2xx status, or when no verified primary email exists.
//
// @testcase TestCallbackRejectsBadState never reaches fetch (placeholder coverage).
func (a *Authenticator) fetchEmail(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.emailsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("emails endpoint returned %d", resp.StatusCode)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return strings.ToLower(e.Email), nil
		}
	}
	for _, e := range emails {
		if e.Verified {
			return strings.ToLower(e.Email), nil
		}
	}
	return "", fmt.Errorf("no verified email on the GitHub account")
}

// sessionCookieFor builds a signed, expiring session cookie for the given email.
//
// @arg email The verified email to encode.
// @return *http.Cookie A signed session cookie.
//
// @testcase TestSessionRoundTrip reads back a cookie built here.
func (a *Authenticator) sessionCookieFor(email string) *http.Cookie {
	payload := fmt.Sprintf("%s|%d", email, time.Now().Add(sessionTTL).Unix())
	return a.cookie(sessionCookie, payload+"|"+a.sign(payload), sessionTTL)
}

// sessionEmail returns the email carried by a valid, unexpired session cookie.
//
// @arg r The request whose cookies are inspected.
// @return string The authenticated email, or empty when there is no valid session.
// @return bool True when a valid, unexpired session is present.
//
// @testcase TestSessionRoundTrip reads a valid session.
// @testcase TestRequireRedirectsAnonymous finds no session.
func (a *Authenticator) sessionEmail(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	email, exp, sig, ok := splitSession(c.Value)
	if !ok || !hmac.Equal([]byte(sig), []byte(a.sign(email+"|"+exp))) {
		return "", false
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > ts {
		return "", false
	}
	return email, true
}

// sign returns the hex HMAC-SHA256 of payload using the session secret.
//
// @arg payload The string to authenticate.
// @return string The hex-encoded HMAC.
//
// @testcase TestSessionRoundTrip relies on a valid signature.
func (a *Authenticator) sign(payload string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// cookie builds an http.Cookie with the security attributes shared by all of the
// authenticator's cookies (HttpOnly, SameSite=Lax, Secure on HTTPS base URLs). A
// non-positive ttl expires the cookie immediately.
//
// @arg name The cookie name.
// @arg value The cookie value.
// @arg ttl The lifetime; non-positive deletes the cookie.
// @return *http.Cookie The configured cookie.
//
// @testcase TestLoginRedirectsToGitHub inspects a cookie built here.
// @testcase TestLogoutClearsSession inspects a deletion cookie built here.
func (a *Authenticator) cookie(name, value string, ttl time.Duration) *http.Cookie {
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   strings.HasPrefix(a.baseURL, "https://"),
	}
	if ttl > 0 {
		c.Expires = time.Now().Add(ttl)
		c.MaxAge = int(ttl.Seconds())
	} else {
		c.MaxAge = -1
	}
	return c
}

// splitSession splits a session cookie value into its email, expiry and signature.
//
// @arg value The raw cookie value "email|exp|sig".
// @return string The email portion.
// @return string The expiry (unix seconds) portion.
// @return string The signature portion.
// @return bool True when the value had exactly the three expected parts.
//
// @testcase TestSessionRoundTrip parses a valid value.
func splitSession(value string) (string, string, string, bool) {
	parts := strings.Split(value, "|")
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// randomToken returns a URL-safe random token for OAuth state and session secrets.
//
// @return string A base64url-encoded 32-byte random token.
// @error error when the system RNG fails.
//
// @testcase TestLoginRedirectsToGitHub uses a generated state token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// safeNext returns next when it is a safe local path (a single leading "/"),
// otherwise "/". This prevents the login flow from being used as an open redirect.
//
// @arg next The requested post-login destination.
// @return string A safe local path.
//
// @testcase TestSafeNext accepts local paths and rejects absolute URLs.
func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
		return next
	}
	return "/"
}
