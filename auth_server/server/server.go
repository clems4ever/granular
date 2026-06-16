// Package server implements the granular authorization server (AS): the generic
// policy authority. It is domain-agnostic — it never parses or understands the
// policies it stores. It exposes:
//
//   - PUT/GET/DELETE /api/policy — a token represents a policy; PUT mints one, GET
//     reads the grants attached to it, DELETE destroys it.
//   - POST /api/proposals — a client (Bearer token) submits a bundle of
//     gateway-signed grant requests; the AS verifies each HMAC against the gateway's
//     shared secret and records a pending proposal, returning a review URL.
//   - GET/POST /proposal/{id} — the human consent screen, gated on the approver email.
//   - POST /api/verify — a gateway asks whether an operation is authorized by the
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

	"github.com/clems4ever/granular/auth_server/server/web"
	"github.com/clems4ever/granular/auth_server/store"
	"github.com/clems4ever/granular/internal/proposal"
)

// Server wires together the store, the registered gateway HMAC secrets and the public
// base URL used to build review links. An optional authenticator guards the human
// consent pages behind a GitHub login.
type Server struct {
	store    *store.Store
	baseURL  string
	gateways map[string]string // gateway id -> shared HMAC secret
	auth     *Authenticator
}

// proposalInput is the body a client posts to POST /api/proposals: the email of the
// human who must approve, and the gateway-signed grant requests to bundle.
type proposalInput struct {
	ApproverEmail string                        `json:"approver_email"`
	Items         []proposal.SignedGrantRequest `json:"items"`
}

// proposalOutput is returned by POST /api/proposals.
type proposalOutput struct {
	ProposalID string `json:"proposal_id"`
	URL        string `json:"url"`
	Error      string `json:"error,omitempty"`
}

// policyOutput is returned by PUT /api/policy (Token only) and GET /api/policy (the
// attached grants).
type policyOutput struct {
	Token  string      `json:"token,omitempty"`
	Grants []grantView `json:"grants,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// grantView is one active grant returned to the policy holder, carrying the opaque,
// gateway-signed item the gateway re-checks at enforcement.
type grantView struct {
	GatewayID string                      `json:"gateway_id"`
	ExpiresAt string                      `json:"expires_at"`
	Item      proposal.SignedGrantRequest `json:"item"`
}

// verifyInput is the body a gateway posts to POST /api/verify: the policy token and
// the authorization questions plus the entity world to evaluate them against.
type verifyInput struct {
	Token    string         `json:"token"`
	Requests []requestInput `json:"requests"`
	Entities []entityInput  `json:"entities"`
}

// verifyOutput is the AS's decision for POST /api/verify.
type verifyOutput struct {
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`
}

// New creates a Server.
//
// @arg st The store consulted and updated by the handlers.
// @arg baseURL The externally reachable base URL, used to build review links.
// @arg gateways The registered gateway id→secret map used to verify signatures.
// @return *Server A configured server whose Handler can be mounted.
//
// @testcase TestProposalApproveFlow constructs a server.
func New(st *store.Store, baseURL string, gateways map[string]string) *Server {
	return &Server{store: st, baseURL: baseURL, gateways: gateways}
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

// Handler builds the HTTP routing for the AS.
//
// @return http.Handler A mux routing the policy/proposal/verify API and consent UI.
//
// @testcase TestProposalApproveFlow exercises the API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Policy resource: a token represents a policy.
	mux.HandleFunc("PUT /api/policy", s.handleCreatePolicy)
	mux.HandleFunc("GET /api/policy", s.handleGetPolicy)
	mux.HandleFunc("DELETE /api/policy", s.handleDestroyPolicy)

	// Client submits a proposal (Bearer token); gateway verifies an operation.
	mux.HandleFunc("POST /api/proposals", s.handleProposal)
	mux.HandleFunc("POST /api/verify", s.handleVerify)

	mux.Handle("GET /static/", web.Static())

	// GitHub OAuth login endpoints (public, only registered when enabled).
	if s.auth != nil && s.auth.Enabled() {
		mux.HandleFunc("GET "+loginPath, s.auth.handleLogin)
		mux.HandleFunc("GET "+callbackPath, s.auth.handleCallback)
		mux.HandleFunc("POST "+logoutPath, s.auth.handleLogout)
	}

	// Human consent pages require a GitHub login when authentication is enabled.
	mux.Handle("GET /proposal/{id}", s.protect(s.handleApprovePage))
	mux.Handle("POST /proposal/{id}", s.protect(s.handleApproveSubmit))
	mux.HandleFunc("GET /{$}", s.handleIndex)
	return mux
}

// handleCreatePolicy handles PUT /api/policy: it mints a new policy and returns its
// token. The holder presents the token as a bearer credential thereafter.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestProposalApproveFlow creates a policy token.
func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	token, err := s.store.CreatePolicy()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, policyOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, policyOutput{Token: token})
}

// handleGetPolicy handles GET /api/policy: it returns the active grants attached to
// the bearer token, so the holder (or its gateway) can inspect the policy.
//
// @arg w The response writer.
// @arg r The request carrying the bearer token.
//
// @testcase TestGetPolicyReturnsGrants returns the attached grants after approval.
func (s *Server) handleGetPolicy(w http.ResponseWriter, r *http.Request) {
	token, ok := s.bearerPolicy(w, r)
	if !ok {
		return
	}
	grants, err := s.store.PolicyForToken(token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, policyOutput{Error: err.Error()})
		return
	}
	out := policyOutput{Grants: make([]grantView, 0, len(grants))}
	for _, g := range grants {
		out.Grants = append(out.Grants, grantView{
			GatewayID: g.Item.GatewayID,
			ExpiresAt: g.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
			Item:      g.Item,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDestroyPolicy handles DELETE /api/policy: it destroys the bearer token's
// policy and all grants attached to it.
//
// @arg w The response writer.
// @arg r The request carrying the bearer token.
//
// @testcase TestDestroyPolicyEndpoint destroys a policy via the endpoint.
func (s *Server) handleDestroyPolicy(w http.ResponseWriter, r *http.Request) {
	token, ok := s.bearerPolicy(w, r)
	if !ok {
		return
	}
	n, err := s.store.DestroyPolicy(token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, policyOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"destroyed": n})
}

// handleProposal handles POST /api/proposals: a client (authenticated by its policy
// token) submits a bundle of gateway-signed grant requests and an approver email. The
// AS verifies each item's HMAC against the named gateway's shared secret (so the
// client cannot tamper or forge), records a pending proposal, and returns a review
// URL for the approver.
//
// @arg w The response writer.
// @arg r The request whose body is a proposalInput, with a Bearer policy token.
//
// @testcase TestProposalApproveFlow submits a valid proposal.
// @testcase TestProposalRejectsBadSignature rejects an item signed with the wrong secret.
// @testcase TestProposalRequiresApproverEmail rejects a missing approver email.
func (s *Server) handleProposal(w http.ResponseWriter, r *http.Request) {
	token, ok := s.bearerPolicy(w, r)
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
		secret, known := s.gateways[item.GatewayID]
		if !known {
			writeJSON(w, http.StatusBadRequest, proposalOutput{Error: "unknown gateway: " + item.GatewayID})
			return
		}
		if !item.Verify([]byte(secret)) {
			writeJSON(w, http.StatusBadRequest, proposalOutput{Error: "invalid signature on item " + itoa(i)})
			return
		}
	}

	p, err := s.store.CreateProposal(token, strings.ToLower(in.ApproverEmail), in.Items)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, proposalOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, proposalOutput{
		ProposalID: p.ID,
		URL:        s.baseURL + "/proposal/" + p.ID,
	})
}

// handleVerify handles POST /api/verify: a registered gateway asks whether an
// operation is authorized by the policy attached to a token. The AS authenticates the
// gateway (HMAC over the body), loads the token's active policies and evaluates the
// gateway-supplied requests against the gateway-supplied entity world, returning the
// decision. The AS never interprets the policies' meaning.
//
// @arg w The response writer.
// @arg r The request whose body is a verifyInput, signed by the gateway.
//
// @testcase TestVerifyAllowsAfterApproval allows once a covering grant is live.
// @testcase TestVerifyRejectsUnknownGateway rejects an unauthenticated caller.
func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	body, ok := s.authenticateGateway(w, r)
	if !ok {
		return
	}
	var in verifyInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, verifyOutput{Error: "invalid request body"})
		return
	}
	grants, err := s.store.PolicyForToken(in.Token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, verifyOutput{Error: err.Error()})
		return
	}
	var policies []string
	for _, g := range grants {
		policies = append(policies, g.Item.Policies...)
	}
	allowed, err := evaluate(policies, in.Entities, in.Requests)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, verifyOutput{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, verifyOutput{Allowed: allowed})
}

// bearerPolicy extracts the Bearer token and checks it identifies a known policy. On
// failure it writes a 401 and returns ok=false.
//
// @arg w The response writer (used to write a 401 on failure).
// @arg r The incoming request.
// @return string The policy token when valid.
// @return bool True when the token identifies a known policy.
//
// @testcase TestGetPolicyReturnsGrants reads a policy with a valid token.
// @testcase TestPolicyRejectsUnknownToken rejects an unknown bearer token.
func (s *Server) bearerPolicy(w http.ResponseWriter, r *http.Request) (string, bool) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" || !s.store.PolicyExists(token) {
		writeJSON(w, http.StatusUnauthorized, policyOutput{Error: "unknown or missing policy token"})
		return "", false
	}
	return token, true
}

// authenticateGateway reads the request body and verifies it carries a valid HMAC
// signature from a registered gateway (X-Gateway-ID + X-Gateway-Signature, the hex
// HMAC-SHA256 of the raw body keyed by the gateway's shared secret). On success it
// returns the body bytes; on failure it writes a 401 and returns ok=false.
//
// @arg w The response writer (used to write a 401 on failure).
// @arg r The incoming gateway request.
// @return []byte The raw request body when authentication succeeds.
// @return bool True when the request is from a registered, correctly-signed gateway.
//
// @testcase TestVerifyAllowsAfterApproval authenticates a correctly-signed gateway.
// @testcase TestVerifyRejectsUnknownGateway rejects an unknown or missigned gateway.
func (s *Server) authenticateGateway(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, verifyOutput{Error: "cannot read body"})
		return nil, false
	}
	secret, known := s.gateways[r.Header.Get("X-Gateway-ID")]
	if !known {
		writeJSON(w, http.StatusUnauthorized, verifyOutput{Error: "unknown gateway"})
		return nil, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(r.Header.Get("X-Gateway-Signature")), []byte(want)) {
		writeJSON(w, http.StatusUnauthorized, verifyOutput{Error: "invalid gateway signature"})
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

// handleIndex serves the landing page describing the authorization server.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestIndexServesLanding renders the landing page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>granular · authorization server</title>`+
		`<link rel="stylesheet" href="/static/style.css">`+
		`<main class="container narrow"><div class="card">`+
		`<p class="eyebrow">Authorization server</p>`+
		`<h1>granular</h1>`+
		`<p class="lead">The generic policy authority. Clients submit gateway-signed grant requests here `+
		`for human approval, and gateways verify operations against the approved policy before executing them.</p>`+
		`<p class="muted">Approval links are sent to you by the agent; there is nothing to do on this page.</p>`+
		`</div></main>`)
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
