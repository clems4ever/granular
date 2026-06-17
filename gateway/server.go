package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/internal/verify"
)

// Verifier asks the AS whether an operation's requirements are authorized by the policy
// attached to a token. The asclient.Client implements it; tests stub it.
type Verifier interface {
	Verify(ctx context.Context, in verify.Input) (bool, error)
}

// Config is everything needed to build a Gateway: the domain Schema, the action
// Registry, the gateway's identity and shared secret, and the AS Verifier.
type Config struct {
	Schema    Schema
	Registry  *Registry
	GatewayID string
	Secret    []byte
	Verifier  Verifier
}

// Gateway is a configured gateway whose Handler serves the schema, signs grant requests,
// and executes operations after the AS authorizes them.
type Gateway struct {
	schema   Schema
	registry *Registry
	id       string
	secret   []byte
	verifier Verifier
}

// New builds a Gateway from its configuration.
//
// @arg cfg The gateway configuration (schema, registry, identity, secret, verifier).
// @return *Gateway A gateway whose Handler can be mounted on an HTTP server.
//
// @testcase TestSchemaServed builds a gateway and serves its schema.
func New(cfg Config) *Gateway {
	return &Gateway{
		schema:   cfg.Schema,
		registry: cfg.Registry,
		id:       cfg.GatewayID,
		secret:   cfg.Secret,
		verifier: cfg.Verifier,
	}
}

// Handler builds the HTTP routing for the gateway.
//
// @return http.Handler A mux routing the schema, sign and operations endpoints.
//
// @testcase TestSchemaServed exercises the schema route.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schema", g.handleSchema)
	mux.HandleFunc("POST /api/grant-requests/sign", g.handleSign)
	mux.HandleFunc("POST /api/operations", g.handleOperation)
	return mux
}

// handleSchema handles GET /api/schema: it returns the permission vocabulary a client
// reads to build a grant request.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestSchemaServed returns the schema.
func (g *Gateway) handleSchema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, g.schema)
}

// handleSign handles POST /api/grant-requests/sign: a client posts the capability bundle
// it built from the schema; the gateway translates it into Cedar policies and a
// human-readable presentation, signs the two together with its shared secret, and
// returns the SignedGrantRequest. The client bundles it into a proposal to the AS but
// cannot tamper with it (it holds no secret).
//
// @arg w The response writer.
// @arg r The request whose body is a GrantRequest (capabilities).
//
// @testcase TestSignProducesVerifiableRequest signs a capability bundle.
// @testcase TestSignRejectsUnknownAction rejects a capability naming an unknown action.
func (g *Gateway) handleSign(w http.ResponseWriter, r *http.Request) {
	var req GrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid request body"))
		return
	}
	policies, err := policiesFromCapabilities(g.schema, req.Capabilities)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	pres := buildPresentation(g.schema, req.Reason, req.Capabilities)
	signed := proposal.Sign(g.secret, g.id, pres, policies)
	writeJSON(w, http.StatusOK, signed)
}

// handleOperation handles POST /api/operations: a client asks the gateway to run an
// operation, presenting its AS policy token as a bearer credential. The gateway builds
// the operation, derives its requirements, asks the AS whether the token's policy
// authorizes them, and executes only on an allow.
//
// @arg w The response writer.
// @arg r The request whose body is an OperationRequest, with a Bearer policy token.
//
// @testcase TestOperationDenied returns 403 when the AS denies.
// @testcase TestOperationAllowed executes when the AS allows.
func (g *Gateway) handleOperation(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, errBody("missing policy token"))
		return
	}
	var req OperationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("invalid request body"))
		return
	}
	op, err := g.registry.Build(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}

	reqs := op.Requirements()
	allowed, err := g.verifier.Verify(r.Context(), verify.Input{
		Token:    token,
		Requests: verifyRequests(g.schema, reqs),
		Entities: verifyWorld(g.schema, reqs),
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
