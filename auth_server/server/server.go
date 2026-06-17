// Package server implements the granular authorization server (AS): the generic
// policy authority. It is domain-agnostic — it never parses or understands the
// policies it stores. It exposes:
//
//   - PUT/GET/DELETE /api/subject — a token represents a subject; PUT mints one, GET
//     reads the grants attached to it, DELETE destroys it.
//   - POST /api/proposals — a client (Bearer token) submits a bundle of
//     resource server-signed grant requests; the AS verifies each HMAC against the resource server's
//     shared secret and records a pending proposal, returning a review URL.
//   - GET/POST /proposal/{id} — the human consent screen, gated on the approver email.
//   - POST /api/verify — a resource server asks whether an operation is authorized by the
//     policy attached to a token; the AS evaluates the opaque policies generically.
package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/clems4ever/granular/auth_server/server/web"
	"github.com/clems4ever/granular/auth_server/store"
	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/internal/redoc"
	"github.com/clems4ever/granular/internal/verify"
)

// Server wires together the store, the registered resource server HMAC secrets and the public
// base URL used to build review links. An optional authenticator guards the human
// consent pages behind a GitHub login.
type Server struct {
	store           *store.Store
	baseURL         string
	resourceServers map[string]string // resource server id -> shared HMAC secret
	adminToken      string            // bearer gating the subject-administration endpoints
	requestTTL      time.Duration     // how long a submitted proposal stays pending
	auth            *Authenticator
}

// defaultRequestTTL is the pending lifetime applied to a proposal when the server is not
// configured otherwise.
const defaultRequestTTL = 15 * time.Minute

// proposalInput is the body a client posts to POST /api/proposals: the email of the
// human who must approve, and the resource server-signed grant requests to bundle.
type proposalInput struct {
	ApproverEmail string                        `json:"approver_email"`
	Items         []proposal.SignedGrantRequest `json:"items"`
}

// proposalOutput is returned by POST /api/proposals.
type proposalOutput struct {
	ProposalID string `json:"proposal_id"`
	URL        string `json:"url"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	Error      string `json:"error,omitempty"`
}

// subjectOutput is returned by PUT /api/subject (Token only) and GET /api/subject (the
// attached grants).
type subjectOutput struct {
	Token  string      `json:"token,omitempty"`
	Grants []grantView `json:"grants,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// grantView is one active grant returned to the subject-token holder, carrying the opaque,
// resource server-signed item the resource server re-checks at enforcement.
type grantView struct {
	SubjectToken     string                      `json:"subject_token,omitempty"`
	ResourceServerID string                      `json:"resource_server_id"`
	ExpiresAt        string                      `json:"expires_at"`
	Item             proposal.SignedGrantRequest `json:"item"`
}

// activityOutput is returned by GET /api/activity (admin-gated): the full grant inventory
// and the request/decision history across every subject.
type activityOutput struct {
	Grants  []grantView    `json:"grants"`
	History []historyEntry `json:"history"`
	Error   string         `json:"error,omitempty"`
}

// historyEntry is one proposal in the admin activity history, including the subject it was
// submitted for and the approver it named.
type historyEntry struct {
	SubjectToken string `json:"subject_token"`
	Approver     string `json:"approver"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	Items        int    `json:"items"`
	CreatedAt    string `json:"created_at"`
}

// New creates a Server.
//
// @arg st The store consulted and updated by the handlers.
// @arg baseURL The externally reachable base URL, used to build review links.
// @arg resourceServers The registered resource server id→secret map used to verify signatures.
// @return *Server A configured server whose Handler can be mounted.
//
// @testcase TestProposalApproveFlow constructs a server.
func New(st *store.Store, baseURL string, resourceServers map[string]string) *Server {
	return &Server{store: st, baseURL: baseURL, resourceServers: resourceServers, requestTTL: defaultRequestTTL}
}

// UseRequestTTL sets how long a submitted proposal stays pending before it is
// automatically revoked. A non-positive value leaves the default in place.
//
// @arg ttl The pending lifetime for new proposals.
//
// @testcase TestProposalExpiresViaEndpoint configures a short request TTL.
func (s *Server) UseRequestTTL(ttl time.Duration) {
	if ttl > 0 {
		s.requestTTL = ttl
	}
}

// UseAuth attaches an authenticator that guards the consent pages behind a GitHub
// login. When auth is nil or not enabled, the pages remain open.
//
// @arg auth The authenticator to use; nil leaves the pages unprotected.
//
// @testcase TestRequireRedirectsAnonymous attaches an enabled authenticator.
func (s *Server) UseAuth(auth *Authenticator) {
	s.auth = auth
}

// UseAdminToken sets the bearer token that gates the subject-administration endpoints
// (PUT/GET/DELETE /api/subject). When it is empty those endpoints are disabled (they
// fail closed), so an administrator must configure one to manage subjects.
//
// @arg token The admin bearer token; empty disables subject administration.
//
// @testcase TestSubjectAdminRequiresAdminToken rejects calls without the admin token.
func (s *Server) UseAdminToken(token string) {
	s.adminToken = token
}

// Handler builds the HTTP routing for the AS.
//
// @return http.Handler A mux routing the subject/proposal/verify API and consent UI.
//
// @testcase TestProposalApproveFlow exercises the API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Subject resource: a token represents a subject. These are administrative endpoints
	// gated by the admin token; the subject itself is identified by the path for
	// inspection and destruction.
	mux.HandleFunc("PUT /api/subject", s.handleCreateSubject)
	mux.HandleFunc("GET /api/subject/{token}", s.handleGetSubject)
	mux.HandleFunc("DELETE /api/subject/{token}", s.handleDestroySubject)
	// A subject reads its OWN grants, authenticated by its subject token (not the admin
	// token). More specific than /api/subject/{token}, so it takes precedence.
	mux.HandleFunc("GET /api/subject/me", s.handleGetMySubject)

	// Client submits a proposal (Bearer token); resource server verifies an operation.
	mux.HandleFunc("POST /api/proposals", s.handleProposal)
	mux.HandleFunc("POST /api/verify", s.handleVerify)

	// Operator view (admin-gated): the full cross-subject grant inventory and history.
	mux.HandleFunc("GET /api/activity", s.handleActivityAdmin)

	mux.Handle("GET /static/", web.Static())
	mux.HandleFunc("GET /openapi.yaml", s.handleOpenAPI)
	redoc.Register(mux, "granular authorization server API", "/openapi.yaml")

	// GitHub OAuth login endpoints (public, only registered when enabled).
	if s.auth != nil && s.auth.Enabled() {
		mux.HandleFunc("GET "+loginPath, s.auth.handleLogin)
		mux.HandleFunc("GET "+callbackPath, s.auth.handleCallback)
		mux.HandleFunc("POST "+logoutPath, s.auth.handleLogout)
	}

	// Human consent pages require a GitHub login when authentication is enabled.
	mux.Handle("GET /proposal/{id}", s.protect(s.handleApprovePage))
	mux.Handle("POST /proposal/{id}", s.protect(s.handleApproveSubmit))
	// The single main page: a signed-in approver's own activity, else a landing.
	mux.HandleFunc("GET /{$}", s.handleHome)
	return mux
}

// handleCreateSubject handles PUT /api/subject: an administrator (authenticated by the
// admin token) mints a new subject and receives its token, which it hands to a client to
// present as a bearer credential thereafter.
//
// @arg w The response writer.
// @arg r The incoming request carrying the admin bearer token.
//
// @testcase TestProposalApproveFlow creates a subject token.
// @testcase TestSubjectAdminRequiresAdminToken rejects creation without the admin token.
func (s *Server) handleCreateSubject(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	token, err := s.store.CreateSubject()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, subjectOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, subjectOutput{Token: token})
}

// handleGetSubject handles GET /api/subject/{token}: an administrator inspects the active
// grants attached to the subject named in the path.
//
// @arg w The response writer.
// @arg r The request carrying the admin bearer token and the subject token in the path.
//
// @testcase TestGetSubjectReturnsGrants returns the attached grants after approval.
// @testcase TestSubjectRejectsUnknownToken returns 404 for an unknown subject token.
func (s *Server) handleGetSubject(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	token := r.PathValue("token")
	if !s.store.SubjectExists(token) {
		writeJSON(w, http.StatusNotFound, subjectOutput{Error: "unknown subject token"})
		return
	}
	grants, err := s.store.SubjectForToken(token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, subjectOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, subjectOutput{Grants: grantViews(grants)})
}

// handleGetMySubject handles GET /api/subject/me: a subject reads its OWN active grants,
// authenticated by its subject token (the bearer it already holds) rather than the admin
// token. This lets a sandboxed agent introspect what it currently holds without any
// privileged credential, and it can never see another subject's grants.
//
// @arg w The response writer.
// @arg r The request carrying the subject token as a bearer.
//
// @testcase TestMySubjectReturnsOwnGrants returns the bearer's own grants.
// @testcase TestMySubjectRejectsUnknownToken rejects a missing or unknown bearer token.
func (s *Server) handleGetMySubject(w http.ResponseWriter, r *http.Request) {
	token, ok := s.bearerSubject(w, r)
	if !ok {
		return
	}
	grants, err := s.store.SubjectForToken(token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, subjectOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, subjectOutput{Grants: grantViews(grants)})
}

// grantViews renders store grants as API grant views, omitting the subject token: callers
// of GET /api/subject/{token} and /api/subject/me already identify the subject, so the
// token is not echoed back. The admin activity view sets it explicitly instead.
//
// @arg grants The active grants to render.
// @return []grantView One view per grant.
//
// @testcase TestGetSubjectReturnsGrants renders a subject's grants.
func grantViews(grants []store.Grant) []grantView {
	out := make([]grantView, 0, len(grants))
	for _, g := range grants {
		out = append(out, grantView{
			ResourceServerID: g.Item.ResourceServerID,
			ExpiresAt:        g.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
			Item:             g.Item,
		})
	}
	return out
}

// handleActivityAdmin handles GET /api/activity: an administrator (admin token) retrieves
// the full grant inventory and the request/decision history across ALL subjects. This is
// the privileged operator view; a human approver only ever sees their own history at
// /activity, and a subject only its own grants at /api/subject/me.
//
// @arg w The response writer.
// @arg r The request carrying the admin bearer token.
//
// @testcase TestActivityAdminRequiresAdminToken rejects calls without the admin token.
// @testcase TestActivityAdminReturnsInventory returns grants and history after approval.
func (s *Server) handleActivityAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	grants, err := s.store.AllGrants()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, activityOutput{Error: err.Error()})
		return
	}
	proposals, err := s.store.AllProposals()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, activityOutput{Error: err.Error()})
		return
	}
	now := time.Now()
	out := activityOutput{
		Grants:  make([]grantView, 0, len(grants)),
		History: make([]historyEntry, 0, len(proposals)),
	}
	for _, g := range grants {
		out.Grants = append(out.Grants, grantView{
			SubjectToken:     g.Token,
			ResourceServerID: g.Item.ResourceServerID,
			ExpiresAt:        g.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
			Item:             g.Item,
		})
	}
	for _, p := range proposals {
		status := p.Status
		if p.Expired(now) {
			status = store.StatusExpired
		}
		out.History = append(out.History, historyEntry{
			SubjectToken: p.Token,
			Approver:     p.ApproverEmail,
			Status:       string(status),
			Summary:      firstSummary(p.Items),
			Items:        len(p.Items),
			CreatedAt:    fmtTime(p.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDestroySubject handles DELETE /api/subject/{token}: an administrator destroys the
// subject named in the path and all grants attached to it.
//
// @arg w The response writer.
// @arg r The request carrying the admin bearer token and the subject token in the path.
//
// @testcase TestDestroySubjectEndpoint destroys a subject via the endpoint.
func (s *Server) handleDestroySubject(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	n, err := s.store.DestroySubject(r.PathValue("token"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, subjectOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"destroyed": n})
}

// requireAdmin authenticates a subject-administration request against the configured
// admin token. It fails closed: when no admin token is configured the endpoints are
// disabled (503), and a missing or wrong token is rejected (401). The comparison is
// constant-time.
//
// @arg w The response writer (used to write the error on failure).
// @arg r The incoming request carrying the admin bearer token.
// @return bool True when the request presents the configured admin token.
//
// @testcase TestSubjectAdminRequiresAdminToken rejects missing, wrong, and unconfigured tokens.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.adminToken == "" {
		writeJSON(w, http.StatusServiceUnavailable, subjectOutput{Error: "subject administration is disabled (no admin token configured)"})
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" || !hmac.Equal([]byte(got), []byte(s.adminToken)) {
		writeJSON(w, http.StatusUnauthorized, subjectOutput{Error: "invalid admin token"})
		return false
	}
	return true
}

// handleProposal handles POST /api/proposals: a client (authenticated by its subject
// token) submits a bundle of resource server-signed grant requests and an approver email. The
// AS verifies each item's HMAC against the named resource server's shared secret (so the
// client cannot tamper or forge), records a pending proposal, and returns a review
// URL for the approver.
//
// @arg w The response writer.
// @arg r The request whose body is a proposalInput, with a Bearer subject token.
//
// @testcase TestProposalApproveFlow submits a valid proposal.
// @testcase TestProposalRejectsBadSignature rejects an item signed with the wrong secret.
// @testcase TestProposalRequiresApproverEmail rejects a missing approver email.
func (s *Server) handleProposal(w http.ResponseWriter, r *http.Request) {
	token, ok := s.bearerSubject(w, r)
	if !ok {
		return
	}
	var in proposalInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, proposalOutput{Error: "invalid request body"})
		return
	}
	if in.ApproverEmail == "" {
		writeJSON(w, http.StatusBadRequest, proposalOutput{Error: "approver_email is required"})
		return
	}
	if len(in.Items) == 0 {
		writeJSON(w, http.StatusBadRequest, proposalOutput{Error: "no items"})
		return
	}
	for i, item := range in.Items {
		secret, known := s.resourceServers[item.ResourceServerID]
		if !known {
			writeJSON(w, http.StatusBadRequest, proposalOutput{Error: "unknown resource server: " + item.ResourceServerID})
			return
		}
		if !item.Verify([]byte(secret)) {
			writeJSON(w, http.StatusBadRequest, proposalOutput{Error: "invalid signature on item " + itoa(i)})
			return
		}
	}

	p, err := s.store.CreateProposal(token, strings.ToLower(in.ApproverEmail), in.Items, s.requestTTL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, proposalOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, proposalOutput{
		ProposalID: p.ID,
		URL:        s.baseURL + "/proposal/" + p.ID,
		ExpiresAt:  p.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// handleVerify handles POST /api/verify: a registered resource server asks whether an
// operation is authorized by the subject identified by a token. The AS authenticates the
// resource server (HMAC over the body), loads the token's active policies and evaluates the
// resource server-supplied requests against the resource server-supplied entity world, returning the
// decision. The AS never interprets the policies' meaning.
//
// @arg w The response writer.
// @arg r The request whose body is a verifyInput, signed by the resource server.
//
// @testcase TestVerifyAllowsAfterApproval allows once a covering grant is live.
// @testcase TestVerifyRejectsUnknownResourceServer rejects an unauthenticated caller.
func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	body, ok := s.authenticateResourceServer(w, r)
	if !ok {
		return
	}
	var in verify.Input
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, verify.Output{Error: "invalid request body"})
		return
	}
	grants, err := s.store.SubjectForToken(in.Token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, verify.Output{Error: err.Error()})
		return
	}
	var policies []string
	for _, g := range grants {
		policies = append(policies, g.Item.Policies...)
	}
	allowed, err := evaluate(policies, in.Entities, in.Requests)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, verify.Output{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, verify.Output{Allowed: allowed})
}

// bearerSubject extracts the Bearer token and checks it identifies a known subject. On
// failure it writes a 401 and returns ok=false.
//
// @arg w The response writer (used to write a 401 on failure).
// @arg r The incoming request.
// @return string The subject token when valid.
// @return bool True when the token identifies a known subject.
//
// @testcase TestGetSubjectReturnsGrants reads a subject with a valid token.
// @testcase TestSubjectRejectsUnknownToken rejects an unknown bearer token.
func (s *Server) bearerSubject(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" || !s.store.SubjectExists(token) {
		writeJSON(w, http.StatusUnauthorized, subjectOutput{Error: "unknown or missing subject token"})
		return "", false
	}
	return token, true
}

// authenticateResourceServer reads the request body and verifies it carries a valid HMAC
// signature from a registered resource server (X-Resource-Server-ID + X-Resource-Server-Signature, the hex
// HMAC-SHA256 of the raw body keyed by the resource server's shared secret). On success it
// returns the body bytes; on failure it writes a 401 and returns ok=false.
//
// @arg w The response writer (used to write a 401 on failure).
// @arg r The incoming resource server request.
// @return []byte The raw request body when authentication succeeds.
// @return bool True when the request is from a registered, correctly-signed resource server.
//
// @testcase TestVerifyAllowsAfterApproval authenticates a correctly-signed resource server.
// @testcase TestVerifyRejectsUnknownResourceServer rejects an unknown or missigned resource server.
func (s *Server) authenticateResourceServer(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, verify.Output{Error: "cannot read body"})
		return nil, false
	}
	secret, known := s.resourceServers[r.Header.Get("X-Resource-Server-ID")]
	if !known {
		writeJSON(w, http.StatusUnauthorized, verify.Output{Error: "unknown resource server"})
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(r.Header.Get("X-Resource-Server-Signature")), []byte(want)) {
		writeJSON(w, http.StatusUnauthorized, verify.Output{Error: "invalid resource server signature"})
		return nil, false
	}
	return body, true
}

// protect wraps a human-facing handler so it requires a GitHub login when an enabled
// authenticator is attached; otherwise it returns the handler unchanged.
//
// @arg h The page handler to guard.
// @return http.Handler The handler, wrapped with authentication when enabled.
//
// @testcase TestRequireRedirectsAnonymous checks a guarded page redirects.
func (s *Server) protect(h http.HandlerFunc) http.Handler {
	if s.auth == nil {
		return h
	}
	return s.auth.Require(h)
}

// render writes an HTML page wrapped in the shared layout, injecting the signed-in
// user (when authentication is enabled) into the layout chrome.
//
// @arg w The response writer.
// @arg r The request, used to read the current session.
// @arg name The page name to render.
// @arg data The page's own template data.
// @error error when the page is unknown or rendering fails.
//
// @testcase TestApprovePageRendersItems renders the consent page.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) error {
	return web.Render(w, name, s.navFor(r), data)
}

// navFor builds the layout chrome for r: the signed-in email when authentication is
// enabled, or an empty Nav otherwise.
//
// @arg r The request, used to read the current session.
// @return web.Nav The layout chrome for the request.
//
// @testcase TestApprovePageRendersItems reads the nav for a request.
func (s *Server) navFor(r *http.Request) web.Nav {
	if s.auth == nil || !s.auth.Enabled() {
		return web.Nav{}
	}
	email, _ := s.auth.CurrentEmail(r)
	return web.Nav{User: email, AuthEnabled: true}
}

// handleOpenAPI serves the embedded OpenAPI document describing the AS JSON API.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestOpenAPIServed serves the spec over HTTP.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(web.OpenAPISpec())
}

// homeView is the data for the landing page shown to anonymous visitors. SignIn is the
// "log in with GitHub" URL, set only when consent authentication is enabled.
type homeView struct {
	SignIn string
}

// handleHome serves GET /: the single main page. For a signed-in approver it IS their
// approvals view (the requests addressed to them and the decisions made); otherwise it is
// an informational landing — with a "sign in" call to action when consent authentication
// is enabled, so a human can reach their approvals. There is no separate /activity page.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestHomeShowsApproverHistory shows a signed-in approver their own history.
// @testcase TestHomeScopedToApprover hides another approver's requests.
// @testcase TestHomeShowsSignInWhenAnonymous offers a login to an anonymous visitor.
// @testcase TestHomeLandingWhenAuthDisabled renders the landing when auth is off.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if s.auth != nil && s.auth.Enabled() {
		if email, ok := s.auth.CurrentEmail(r); ok && email != "" {
			proposals, err := s.store.AllProposals()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			mine := make([]store.Proposal, 0, len(proposals))
			for _, p := range proposals {
				if p.ApproverEmail == email {
					mine = append(mine, p)
				}
			}
			_ = s.render(w, r, "activity", buildActivity(time.Now(), mine))
			return
		}
	}
	view := homeView{}
	if s.auth != nil && s.auth.Enabled() {
		view.SignIn = loginPath + "?next=%2F"
	}
	_ = s.render(w, r, "home", view)
}

// writeJSON serialises v as JSON with the given status code.
//
// @arg w The response writer.
// @arg status The HTTP status code to send.
// @arg v The value to encode as the JSON body.
//
// @testcase TestProposalRejectsBadSignature observes a JSON error body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// itoa renders a small non-negative int as a decimal string for error messages.
//
// @arg n The integer to render.
// @return string The decimal string.
//
// @testcase TestProposalRejectsBadSignature surfaces an item index via itoa.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
