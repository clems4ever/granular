// Package server implements the granular HTTP server: it receives operation
// attempts, checks grants, mints grant requests, serves a human approval
// page, and executes approved operations.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/authz"
	"github.com/clems4ever/granular/internal/grants"
	"github.com/clems4ever/granular/internal/operations"
	"github.com/clems4ever/granular/internal/server/web"
)

// Server wires together the operation registry, the grant store, the execution
// environment and the public base URL used to build approval links. An optional
// authenticator guards the human-facing web pages behind a GitHub login.
type Server struct {
	registry *operations.Registry
	store    *grants.Store
	env      operations.Env
	baseURL  string
	auth     *Authenticator
}

// New creates a Server.
//
// @arg registry The operation registry used to build operations from requests.
// @arg store The grant/delegation store consulted and updated by handlers.
// @arg env The execution environment (credentials, workspace) for operations.
// @arg baseURL The externally reachable base URL, used to build approval links.
// @return *Server A configured server whose Handler can be mounted.
//
// @testcase TestOperationPendingThenApprovedThenCompleted constructs a server.
func New(registry *operations.Registry, store *grants.Store, env operations.Env, baseURL string) *Server {
	return &Server{registry: registry, store: store, env: env, baseURL: baseURL}
}

// UseAuth attaches an authenticator that guards the web pages behind a GitHub
// login. When auth is nil or not enabled, the pages remain open.
//
// @arg auth The authenticator to use; nil leaves the pages unprotected.
//
// @testcase TestWebPagesRequireAuthWhenEnabled attaches an enabled authenticator.
func (s *Server) UseAuth(auth *Authenticator) {
	s.auth = auth
}

// protect wraps a human-facing handler so it requires a GitHub login when an
// enabled authenticator is attached; otherwise it returns the handler unchanged.
//
// @arg h The page handler to guard.
// @return http.Handler The handler, wrapped with authentication when enabled.
//
// @testcase TestWebPagesRequireAuthWhenEnabled checks a guarded page redirects.
func (s *Server) protect(h http.HandlerFunc) http.Handler {
	if s.auth == nil {
		return h
	}
	return s.auth.Require(h)
}

// render writes an HTML page wrapped in the shared layout, injecting the current
// signed-in user (when authentication is enabled) into the layout chrome so the
// top bar can show the user and a sign-out button.
//
// @arg w The response writer.
// @arg r The request, used to read the current session.
// @arg name The page name to render.
// @arg data The page's own template data.
// @error error when the page is unknown or rendering fails.
//
// @testcase TestSignedInUserShownInNav renders a page showing the signed-in user.
func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data any) error {
	return web.Render(w, name, s.navFor(r), data)
}

// navFor builds the layout chrome for r: the signed-in GitHub user when
// authentication is enabled, or an empty Nav otherwise.
//
// @arg r The request, used to read the current session.
// @return web.Nav The layout chrome for the request.
//
// @testcase TestSignedInUserShownInNav reads the nav for a signed-in request.
func (s *Server) navFor(r *http.Request) web.Nav {
	if s.auth == nil || !s.auth.Enabled() {
		return web.Nav{}
	}
	user, _ := s.auth.sessionUser(r)
	return web.Nav{User: user, AuthEnabled: true}
}

// Handler builds the HTTP routing for the server.
//
// @return http.Handler A mux routing the API and approval endpoints.
//
// @testcase TestOperationPendingThenApprovedThenCompleted exercises the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Agent/CLI API, the git proxy and static assets are not behind the browser
	// login: the agent authenticates per-grant and git has its own credentials.
	mux.HandleFunc("POST /api/operations", s.handleOperation)
	mux.HandleFunc("POST /api/grant-requests", s.handleGrantRequest)
	mux.HandleFunc("GET /api/grant-requests/{id}", s.handleRequestStatus)
	mux.HandleFunc("GET /api/catalog", s.handleCatalogJSON)
	mux.HandleFunc("GET /api/grants", s.handleGrantsJSON)
	mux.HandleFunc("POST /api/grants/{id}/revoke", s.handleRevoke)
	mux.HandleFunc("/git/{rest...}", s.handleGitProxy)
	mux.Handle("GET /static/", web.Static())

	// GitHub OAuth login endpoints (public, only registered when enabled).
	if s.auth != nil && s.auth.Enabled() {
		mux.HandleFunc("GET /auth/login", s.auth.handleLogin)
		mux.HandleFunc("GET /auth/callback", s.auth.handleCallback)
		mux.HandleFunc("POST /auth/logout", s.auth.handleLogout)
	}

	// Human-facing pages require a GitHub login when authentication is enabled.
	mux.Handle("GET /approve/{id}", s.protect(s.handleApprovePage))
	mux.Handle("POST /approve/{id}", s.protect(s.handleApproveSubmit))
	mux.Handle("GET /catalog", s.protect(s.handleCatalogPage))
	mux.Handle("GET /grants", s.protect(s.handleGrantsPage))
	mux.Handle("POST /grants/{id}/revoke", s.protect(s.handleRevokeForm))
	mux.Handle("GET /{$}", s.protect(s.handleIndex))
	return mux
}

// handleOperation handles POST /api/operations, the just-in-time path: an agent
// asks the server to perform an operation now. When live grants already authorise
// it the operation executes immediately; otherwise the server creates a pending
// grant request scoped to exactly that operation's requirements and returns its
// approval URL, so a later retry of the same operation executes once a human has
// approved.
//
// @arg w The response writer.
// @arg r The request whose body is an api.Operation.
//
// @testcase TestOperationPendingThenApprovedThenCompleted drives this end to end.
// @testcase TestOperationUnknownTypeIsBadRequest posts an unregistered type.
func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	var req api.Operation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.RequestResponse{Error: "invalid request body"})
		return
	}
	op, err := s.registry.Build(req, s.env)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, api.RequestResponse{Error: err.Error()})
		return
	}

	allowed, err := s.authorize(op.Requirements())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.RequestResponse{Error: err.Error()})
		return
	}
	if !allowed {
		proposed := authz.MinimalPermits(authz.Principal(), op.Requirements())
		dr, err := s.store.CreateRequest(op.Type(), op.Describe(), proposed, req.Params)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, api.RequestResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, api.RequestResponse{
			Status:      api.StatusPending,
			RequestID:   dr.ID,
			ApprovalURL: s.baseURL + "/approve/" + dr.ID,
		})
		return
	}

	result, err := op.Execute(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.RequestResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, api.RequestResponse{Status: api.StatusCompleted, Result: result})
}

// authorize reports whether the active stored policies allow every requirement.
//
// @arg reqs The operation's authorization requirements.
// @return bool True when all requirements are allowed by the active policies.
// @error error when policies cannot be loaded or fail to parse.
//
// @testcase TestOperationPendingThenApprovedThenCompleted drives the allow path.
func (s *Server) authorize(reqs []authz.Requirement) (bool, error) {
	policies, err := s.store.ActivePolicies()
	if err != nil {
		return false, err
	}
	return authz.AllowsAll(policies, authz.Principal(), reqs)
}

// handleRequestStatus handles GET /api/grant-requests/{id}: it reports a pending
// grant request's current status so the CLI can poll.
//
// @arg w The response writer.
// @arg r The request carrying the {id} path value.
//
// @testcase TestOperationPendingThenApprovedThenCompleted polls status between steps.
// @testcase TestRequestStatusNotFound queries an unknown id.
func (s *Server) handleRequestStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dr, err := s.store.GetRequest(id)
	if errors.Is(err, grants.ErrRequestNotFound) {
		writeJSON(w, http.StatusNotFound, api.RequestStatusResponse{RequestID: id, Error: "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.RequestStatusResponse{RequestID: id, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, api.RequestStatusResponse{RequestID: id, Status: dr.Status})
}

// writeJSON serialises v as JSON with the given status code.
//
// @arg w The response writer.
// @arg status The HTTP status code to send.
// @arg v The value to encode as the JSON body.
//
// @testcase TestOperationUnknownTypeIsBadRequest observes a JSON error body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// defaultTTL is the grant lifetime used when the approval form omits or sends an
// invalid duration. It is short by design so grants expire quickly.
const defaultTTL = 2 * time.Minute

// ttlOptions lists the expiration choices offered on the approval page; the first
// entry is the default selected option.
var ttlOptions = []struct {
	Label string
	Value string
}{
	{"2 minutes", "2m"},
	{"15 minutes", "15m"},
	{"1 hour", "1h"},
	{"8 hours", "8h"},
	{"24 hours", "24h"},
}

// parseTTL converts an approval-form duration value into a time.Duration, falling
// back to defaultTTL (2 minutes) for empty or invalid input.
//
// @arg value The raw duration string from the form, e.g. "2m".
// @return time.Duration The parsed duration, or defaultTTL on failure.
//
// @testcase TestParseTTLFallsBack checks empty and invalid values default to 2m.
func parseTTL(value string) time.Duration {
	if value == "" {
		return defaultTTL
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return defaultTTL
	}
	return d
}
