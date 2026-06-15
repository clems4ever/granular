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

	"github.com/clems4ever/granular/internal/server/web"
)

// GitHub OAuth endpoints. They are fields on the Authenticator so tests can point
// them at a stub server; these are the production defaults.
const (
	defaultGitHubAuthorizeURL = "https://github.com/login/oauth/authorize"
	defaultGitHubTokenURL     = "https://github.com/login/oauth/access_token"
	defaultGitHubUserURL      = "https://api.github.com/user"
)

const (
	// sessionCookie holds the signed login session for the web UI.
	sessionCookie = "granular_session"
	// stateCookie holds the anti-CSRF OAuth state plus the post-login destination.
	stateCookie = "granular_oauth_state"
	// sessionTTL is how long a web login lasts before re-authentication.
	sessionTTL = 12 * time.Hour
)

// AuthConfig configures GitHub-OAuth protection of the human-facing web pages.
type AuthConfig struct {
	ClientID      string
	ClientSecret  string
	AllowedUsers  []string
	SessionSecret []byte
	BaseURL       string
}

// Authenticator guards the web pages behind a "log in with GitHub" flow, admitting
// only an allowlist of GitHub usernames and tracking the session in a signed cookie.
type Authenticator struct {
	clientID     string
	clientSecret string
	allowed      map[string]bool
	secret       []byte
	baseURL      string
	httpClient   *http.Client

	authorizeURL string
	tokenURL     string
	userURL      string
}

// NewAuthenticator builds an Authenticator from cfg. It is only "enabled" when both
// a client id and secret are present; otherwise the pages stay open and the caller
// is expected to warn. Allowed usernames are matched case-insensitively, and a
// random session secret is generated when none is supplied.
//
// @arg cfg The OAuth credentials, username allowlist, session secret and base URL.
// @return *Authenticator A configured authenticator (possibly disabled).
//
// @testcase TestAuthenticatorDisabledWhenUnconfigured builds one without credentials.
// @testcase TestRequireAllowsValidSession builds an enabled one.
func NewAuthenticator(cfg AuthConfig) *Authenticator {
	allowed := make(map[string]bool, len(cfg.AllowedUsers))
	for _, u := range cfg.AllowedUsers {
		if u = strings.ToLower(strings.TrimSpace(u)); u != "" {
			allowed[u] = true
		}
	}
	secret := cfg.SessionSecret
	if len(secret) == 0 {
		secret = make([]byte, 32)
		_, _ = rand.Read(secret)
	}
	return &Authenticator{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		allowed:      allowed,
		secret:       secret,
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		authorizeURL: defaultGitHubAuthorizeURL,
		tokenURL:     defaultGitHubTokenURL,
		userURL:      defaultGitHubUserURL,
	}
}

// Enabled reports whether OAuth protection is configured (client id and secret set).
//
// @return bool True when the web UI should require a GitHub login.
//
// @testcase TestAuthenticatorDisabledWhenUnconfigured checks the disabled case.
func (a *Authenticator) Enabled() bool {
	return a.clientID != "" && a.clientSecret != ""
}

// AllowedCount returns the number of GitHub usernames in the allowlist, so the
// server can warn when authentication is enabled but no user can ever pass.
//
// @return int The size of the username allowlist.
//
// @testcase TestIsAllowed populates and checks the allowlist.
func (a *Authenticator) AllowedCount() int {
	return len(a.allowed)
}

// Require wraps next so that requests without a valid session for an allowed user
// are redirected into the GitHub login flow. When authentication is disabled it
// passes requests straight through.
//
// @arg next The handler to protect.
// @return http.Handler A handler that enforces authentication before next.
//
// @testcase TestRequireRedirectsAnonymous redirects a request with no session.
// @testcase TestRequireAllowsValidSession serves next for a signed-in allowed user.
// @testcase TestRequirePassesThroughWhenDisabled serves next when auth is off.
func (a *Authenticator) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		if user, ok := a.sessionUser(r); ok && a.isAllowed(user) {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/auth/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
	})
}

// isAllowed reports whether login is in the configured allowlist.
//
// @arg login A GitHub username.
// @return bool True when the user may access the web UI.
//
// @testcase TestIsAllowed checks listed and unlisted users.
func (a *Authenticator) isAllowed(login string) bool {
	return a.allowed[strings.ToLower(login)]
}

// handleLogin starts the GitHub OAuth flow: it stores an anti-CSRF state and the
// post-login destination in a short-lived cookie, then redirects to GitHub.
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
	q.Set("redirect_uri", a.baseURL+"/auth/callback")
	q.Set("scope", "read:user")
	q.Set("state", state)
	q.Set("allow_signup", "false")
	http.Redirect(w, r, a.authorizeURL+"?"+q.Encode(), http.StatusFound)
}

// handleCallback completes the GitHub OAuth flow: it validates the state, exchanges
// the code for an access token, fetches the GitHub username and — if the user is in
// the allowlist — sets a signed session cookie and redirects to the saved
// destination. Disallowed users get a 403 page.
//
// @arg w The response writer.
// @arg r The callback request carrying code and state.
//
// @testcase TestCallbackSetsSessionForAllowedUser drives a successful login.
// @testcase TestCallbackDeniesDisallowedUser rejects a user not in the allowlist.
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
	login, err := a.fetchUser(r.Context(), token)
	if err != nil {
		http.Error(w, "failed to fetch GitHub user", http.StatusBadGateway)
		return
	}
	if !a.isAllowed(login) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = web.Render(w, "denied", deniedView{User: login})
		return
	}

	http.SetCookie(w, a.sessionCookieFor(login))
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
// @testcase TestCallbackSetsSessionForAllowedUser exercises a successful exchange.
func (a *Authenticator) exchangeCode(ctx context.Context, code string) (string, error) {
	form := url.Values{}
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", a.baseURL+"/auth/callback")

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

// fetchUser returns the authenticated GitHub user's login for the access token.
//
// @arg ctx Context for cancellation.
// @arg token A GitHub access token.
// @return string The user's GitHub login.
// @error error on transport failure, a non-2xx status, or a missing login.
//
// @testcase TestCallbackSetsSessionForAllowedUser exercises a successful fetch.
func (a *Authenticator) fetchUser(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.userURL, nil)
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
		return "", fmt.Errorf("user endpoint returned %d", resp.StatusCode)
	}
	var out struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Login == "" {
		return "", fmt.Errorf("no login in GitHub user response")
	}
	return out.Login, nil
}

// sessionCookieFor builds a signed, expiring session cookie for the given login.
//
// @arg login The GitHub login to encode.
// @return *http.Cookie A signed session cookie.
//
// @testcase TestRequireAllowsValidSession uses a cookie built here.
func (a *Authenticator) sessionCookieFor(login string) *http.Cookie {
	payload := fmt.Sprintf("%s|%d", login, time.Now().Add(sessionTTL).Unix())
	return a.cookie(sessionCookie, payload+"|"+a.sign(payload), sessionTTL)
}

// sessionUser returns the GitHub login carried by a valid, unexpired session cookie.
//
// @arg r The request whose cookies are inspected.
// @return string The authenticated login, or empty when there is no valid session.
// @return bool True when a valid, unexpired session is present.
//
// @testcase TestRequireAllowsValidSession reads a valid session.
// @testcase TestRequireRedirectsAnonymous finds no session.
func (a *Authenticator) sessionUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	login, exp, sig, ok := splitSession(c.Value)
	if !ok || !hmac.Equal([]byte(sig), []byte(a.sign(login+"|"+exp))) {
		return "", false
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > ts {
		return "", false
	}
	return login, true
}

// sign returns the hex HMAC-SHA256 of payload using the session secret.
//
// @arg payload The string to authenticate.
// @return string The hex-encoded HMAC.
//
// @testcase TestRequireAllowsValidSession relies on a valid signature.
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

// deniedView is the data for the access-denied page.
type deniedView struct {
	User string
}

// splitSession splits a session cookie value into its login, expiry and signature.
//
// @arg value The raw cookie value "login|exp|sig".
// @return string The login portion.
// @return string The expiry (unix seconds) portion.
// @return string The signature portion.
// @return bool True when the value had exactly the three expected parts.
//
// @testcase TestRequireAllowsValidSession parses a valid value.
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
