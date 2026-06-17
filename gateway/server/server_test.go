package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/operations"
	githubops "github.com/clems4ever/granular/internal/operations/github"
	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/internal/verify"
)

const gwSecret = "s3cret"

// stubVerifier records the last token and returns a fixed decision.
type stubVerifier struct {
	allow    bool
	gotToken string
}

// Verify records the token and returns the fixed decision.
//
// @arg ctx Context (unused).
// @arg in The verify input.
// @return bool The fixed decision.
// @error error always nil.
//
// @testcase TestOperationDeniedWithoutGrant drives the deny decision.
func (s *stubVerifier) Verify(ctx context.Context, in verify.Input) (bool, error) {
	s.gotToken = in.Token
	return s.allow, nil
}

// newGateway builds a gateway registered for github.clone with a stub verifier.
//
// @arg allow Whether the stub verifier allows operations.
// @return *Server The gateway under test.
// @return *stubVerifier The injected verifier.
//
// @testcase TestOperationExecutesWhenAllowed builds a gateway with this helper.
func newGateway(allow bool) (*Server, *stubVerifier) {
	reg := operations.NewRegistry()
	reg.Register(githubops.TypeClone, githubops.Clone)
	sv := &stubVerifier{allow: allow}
	return New("gw", []byte(gwSecret), reg, operations.Env{BaseURL: "http://gw"}, sv), sv
}

// post sends a JSON body to the handler and returns the response.
//
// @arg t The test handle.
// @arg h The handler.
// @arg path The request path.
// @arg body The request body.
// @arg bearer A bearer token, or "".
// @return *http.Response The response.
//
// @testcase TestSignProducesVerifiableRequest posts through here.
func post(t *testing.T, h http.Handler, path string, body []byte, bearer string) *http.Response {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

// readCap is the capability used by the sign tests.
//
// @return api.Capability A repo-scoped issues.read capability.
//
// @testcase TestSignProducesVerifiableRequest signs this capability.
func readCap() api.Capability {
	return api.Capability{
		Actions:  []string{"issues.read"},
		Resource: api.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "octocat", "name": "Hello-World"}},
	}
}

// TestSchemaServed checks GET /api/schema returns the catalog.
func TestSchemaServed(t *testing.T) {
	srv, _ := newGateway(true)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/schema")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var obj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if len(obj) == 0 {
		t.Fatal("empty schema")
	}
}

// TestSignProducesVerifiableRequest signs a capability bundle and checks the result
// verifies under the gateway secret and is attributed to the gateway.
func TestSignProducesVerifiableRequest(t *testing.T) {
	srv, _ := newGateway(true)
	body, _ := json.Marshal(api.GrantRequest{Reason: "work", Capabilities: []api.Capability{readCap()}})
	resp := post(t, srv.Handler(), "/api/grant-requests/sign", body, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var sgr proposal.SignedGrantRequest
	if err := json.NewDecoder(resp.Body).Decode(&sgr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sgr.GatewayID != "gw" {
		t.Fatalf("gateway id = %q, want gw", sgr.GatewayID)
	}
	if !sgr.Verify([]byte(gwSecret)) {
		t.Fatal("signed request does not verify under the gateway secret")
	}
	if len(sgr.Policies) == 0 {
		t.Fatal("no policies produced")
	}
}

// TestSignRejectsUnknownAction rejects a capability naming an action not in the catalog.
func TestSignRejectsUnknownAction(t *testing.T) {
	srv, _ := newGateway(true)
	bad := api.Capability{Actions: []string{"bogus.action"}, Resource: readCap().Resource}
	body, _ := json.Marshal(api.GrantRequest{Capabilities: []api.Capability{bad}})
	resp := post(t, srv.Handler(), "/api/grant-requests/sign", body, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestOperationDeniedWithoutGrant returns 403 when the AS denies.
func TestOperationDeniedWithoutGrant(t *testing.T) {
	srv, _ := newGateway(false)
	body, _ := json.Marshal(api.Operation{Type: githubops.TypeClone, Params: map[string]any{"repo": "octocat/Hello-World"}})
	resp := post(t, srv.Handler(), "/api/operations", body, "tok")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestOperationExecutesWhenAllowed executes and returns a result when the AS allows,
// passing the bearer policy token through to the verifier.
func TestOperationExecutesWhenAllowed(t *testing.T) {
	srv, sv := newGateway(true)
	body, _ := json.Marshal(api.Operation{Type: githubops.TypeClone, Params: map[string]any{"repo": "octocat/Hello-World"}})
	resp := post(t, srv.Handler(), "/api/operations", body, "tok")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if sv.gotToken != "tok" {
		t.Fatalf("verifier saw token %q, want tok", sv.gotToken)
	}
	var out struct {
		Status string         `json:"status"`
		Result map[string]any `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Status != "completed" || out.Result["clone_url"] == nil {
		t.Fatalf("unexpected result: %+v", out)
	}
}
