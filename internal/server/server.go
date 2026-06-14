// Package server implements the granular HTTP server: it receives operation
// attempts, checks grants, mints delegation requests, serves a human approval
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
// environment and the public base URL used to build approval links.
type Server struct {
	registry *operations.Registry
	store    *grants.Store
	env      operations.Env
	baseURL  string
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

// Handler builds the HTTP routing for the server.
//
// @return http.Handler A mux routing the API and approval endpoints.
//
// @testcase TestOperationPendingThenApprovedThenCompleted exercises the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/operations", s.handleOperation)
	mux.HandleFunc("POST /api/permissions", s.handlePermissions)
	mux.HandleFunc("GET /api/requests/{id}", s.handleRequestStatus)
	mux.HandleFunc("GET /approve/{id}", s.handleApprovePage)
	mux.HandleFunc("POST /approve/{id}", s.handleApproveSubmit)
	mux.HandleFunc("/git/{rest...}", s.handleGitProxy)
	mux.HandleFunc("GET /catalog", s.handleCatalogPage)
	mux.HandleFunc("GET /api/catalog", s.handleCatalogJSON)
	mux.HandleFunc("GET /grants", s.handleGrantsPage)
	mux.HandleFunc("GET /api/grants", s.handleGrantsJSON)
	mux.HandleFunc("POST /api/grants/{id}/revoke", s.handleRevoke)
	mux.HandleFunc("POST /grants/{id}/revoke", s.handleRevokeForm)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", web.Static())
	return mux
}

// handleOperation handles POST /api/operations: it executes the operation when a
// live grant exists, otherwise creates a pending delegation request.
//
// @arg w The response writer.
// @arg r The request whose body is an api.OperationRequest.
//
// @testcase TestOperationPendingThenApprovedThenCompleted drives this endpoint end to end.
// @testcase TestOperationUnknownTypeIsBadRequest posts an unregistered type.
func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	var req api.OperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.OperationResponse{Error: "invalid request body"})
		return
	}

	op, err := s.registry.Build(req, s.env)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, api.OperationResponse{Error: err.Error()})
		return
	}

	allowed, err := s.authorize(op.Requirements())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.OperationResponse{Error: err.Error()})
		return
	}
	if !allowed {
		proposed := authz.MinimalPermits(authz.Principal(), op.Requirements())
		dr, err := s.store.CreateRequest(op.Type(), op.Describe(), proposed, req.Params)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, api.OperationResponse{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, api.OperationResponse{
			Status:      api.StatusPending,
			RequestID:   dr.ID,
			ApprovalURL: s.baseURL + "/approve/" + dr.ID,
		})
		return
	}

	result, err := op.Execute(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.OperationResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, api.OperationResponse{Status: api.StatusCompleted, Result: result})
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

// handleRequestStatus handles GET /api/requests/{id}: it reports a delegation
// request's current status so the CLI can poll.
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
