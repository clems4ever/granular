// Package server implements the granular gateway (Resource Server): it owns the
// platform credential and the permission vocabulary. It exposes the permission schema
// for clients to build requests, signs grant requests so a client can propose them to
// the AS, and executes operations only after the AS confirms they are authorized.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/authz"
	"github.com/clems4ever/granular/internal/catalog"
	"github.com/clems4ever/granular/internal/operations"
	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/internal/verify"
)

// Verifier asks the AS whether an operation's requirements are authorized by the
// policy attached to a token. asclient.Client implements it; tests stub it.
type Verifier interface {
	Verify(ctx context.Context, in verify.Input) (bool, error)
}

// Server wires the operation registry, execution environment, gateway identity and
// the AS verifier together.
type Server struct {
	gatewayID string
	secret    []byte
	registry  *operations.Registry
	env       operations.Env
	verifier  Verifier
}

// New creates a Server.
//
// @arg gatewayID The gateway's id registered with the AS.
// @arg secret The HMAC secret shared with the AS (signs grant requests).
// @arg registry The operation registry used to build operations.
// @arg env The execution environment (credentials, base URL).
// @arg verifier The AS verify client.
// @return *Server A configured server whose Handler can be mounted.
//
// @testcase TestSignProducesVerifiableRequest constructs a server.
func New(gatewayID string, secret []byte, registry *operations.Registry, env operations.Env, verifier Verifier) *Server {
	return &Server{gatewayID: gatewayID, secret: secret, registry: registry, env: env, verifier: verifier}
}

// Handler builds the HTTP routing for the gateway.
//
// @return http.Handler A mux routing the schema, sign and operations endpoints.
//
// @testcase TestSchemaServed exercises the schema route.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schema", s.handleSchema)
	mux.HandleFunc("POST /api/grant-requests/sign", s.handleSign)
	mux.HandleFunc("POST /api/operations", s.handleOperation)
	return mux
}

// handleSchema handles GET /api/schema: it returns the permission vocabulary (the
// catalog) a client reads to build a grant request.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestSchemaServed returns the catalog.
func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.Build())
}

// handleSign handles POST /api/grant-requests/sign: a client posts the capability
// bundle it built from the schema; the gateway translates it into Cedar policies and
// a human-readable presentation, signs the two together with its shared secret, and
// returns the SignedGrantRequest. The client bundles it into a proposal to the AS but
// cannot tamper with it (it holds no secret).
//
// @arg w The response writer.
// @arg r The request whose body is an api.GrantRequest (capabilities).
//
// @testcase TestSignProducesVerifiableRequest signs a capability bundle.
// @testcase TestSignRejectsUnknownAction rejects a capability naming an unknown action.
func (s *Server) handleSign(w http.ResponseWriter, r *http.Request) {
	var req api.GrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid request body"))
		return
	}
	policies, err := authz.PoliciesFromCapabilities(authz.Principal(), req.Capabilities)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	pres := buildPresentation(req.Reason, req.Capabilities)
	signed := proposal.Sign(s.secret, s.gatewayID, pres, policies)
	writeJSON(w, http.StatusOK, signed)
}

// handleOperation handles POST /api/operations: a client asks the gateway to run an
// operation, presenting its AS policy token as a bearer credential. The gateway builds
// the operation, derives its requirements, asks the AS whether the token's policy
// authorizes them, and executes only on an allow.
//
// @arg w The response writer.
// @arg r The request whose body is an api.Operation, with a Bearer policy token.
//
// @testcase TestOperationDeniedWithoutGrant returns 403 when the AS denies.
// @testcase TestOperationExecutesWhenAllowed executes when the AS allows.
func (s *Server) handleOperation(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, errBody("missing policy token"))
		return
	}
	var req api.Operation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid request body"))
		return
	}
	op, err := s.registry.Build(req, s.env)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}

	reqs := op.Requirements()
	allowed, err := s.verifier.Verify(r.Context(), verify.Input{
		Token:    token,
		Requests: authz.VerifyRequests(reqs),
		Entities: authz.VerifyWorld(reqs),
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody("authorization check failed: "+err.Error()))
		return
	}
	if !allowed {
		writeJSON(w, http.StatusForbidden, map[string]any{"status": "unauthorized"})
		return
	}

	result, err := op.Execute(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "result": result})
}

// errBody builds a small JSON error object.
//
// @arg msg The error message.
// @return map[string]string The error body.
//
// @testcase TestSignRejectsUnknownAction observes an error body.
func errBody(msg string) map[string]string {
	return map[string]string{"error": msg}
}

// writeJSON serialises v as JSON with the given status code.
//
// @arg w The response writer.
// @arg status The HTTP status code to send.
// @arg v The value to encode as the JSON body.
//
// @testcase TestSchemaServed observes a JSON body.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
