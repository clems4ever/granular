package resourceserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/internal/verify"
)

const rsSecret = "s3cret"

// testSchema is a small synthetic vocabulary (an org/repo hierarchy with one read
// action) so the SDK tests never depend on any concrete platform package.
//
// @return Schema A two-resource schema with a scope resolver.
//
// @testcase TestSchemaHelpers inspects this schema.
func testSchema() Schema {
	return Schema{
		AgentType:  "Test::Agent",
		ActionType: "Test::Action",
		AgentID:    "agent",
		Resources: []ResourceType{
			{Name: "t.org", Title: "Org", Entity: "Test::Org"},
			{Name: "t.repo", Title: "Repo", Entity: "Test::Repo", Parent: "t.org"},
		},
		Groups: []Group{{Name: "read", Title: "read", Description: "Read things."}},
		Actions: []Action{
			{Name: "repo.read", Title: "Read repository", Resource: "t.repo", Groups: []string{"read"}, Description: "Read a repo."},
		},
		Templates: []Template{{
			Name: "comment", Title: "Comment", Summary: "Comment on {owner}/{name}",
			Actions: []string{"repo.read"}, Scope: "t.repo",
			Params: []TemplateParam{
				{Name: "owner", Field: "owner", Required: true},
				{Name: "name", Field: "name"},
				{Name: "state", Attr: "state", Op: "eq", Fixed: "open"},
				{Name: "label", Attr: "labels", Op: "contains"},
			},
		}},
		Scope: func(sel ResourceSelector) (string, string, string, error) {
			if sel.Type != "t.repo" {
				return "", "", "", fmt.Errorf("unsupported %q", sel.Type)
			}
			owner := sel.Match["owner"]
			if name := sel.Match["name"]; name != "" && name != "*" {
				full := owner + "/" + name
				return "Test::Repo", full, full, nil
			}
			return "Test::Org", owner, "all repos under " + owner, nil
		},
	}
}

// repoRef builds the resource chain for a t.repo, parented to its t.org.
//
// @arg owner The owner login.
// @arg name The repo name.
// @return ResourceRef The repo reference parented to its org.
//
// @testcase TestVerifyWorld uses this chain.
func repoRef(owner, name string) ResourceRef {
	org := ResourceRef{Type: "t.org", ID: owner}
	return ResourceRef{Type: "t.repo", ID: owner + "/" + name, Parent: &org}
}

// fakeOp is a minimal Operation: one repo.read requirement returning a fixed result.
type fakeOp struct{}

// Requirements returns a single repo.read requirement.
//
// @return []Requirement The fake operation's requirements.
//
// @testcase TestOperationAllowed builds this operation.
func (fakeOp) Requirements() []Requirement {
	return []Requirement{{Action: "repo.read", Resource: repoRef("octo", "hello")}}
}

// Describe returns a fixed description.
//
// @return string A fixed summary.
//
// @testcase TestOperationAllowed reads the description path.
func (fakeOp) Describe() string { return "read octo/hello" }

// Execute returns a fixed result.
//
// @arg ctx Context (unused).
// @return map[string]any A fixed result.
// @error error always nil.
//
// @testcase TestOperationAllowed executes the fake operation.
func (fakeOp) Execute(ctx context.Context) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

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
// @testcase TestOperationDenied drives the deny decision.
func (s *stubVerifier) Verify(ctx context.Context, in verify.Input) (bool, error) {
	s.gotToken = in.Token
	return s.allow, nil
}

// newResourceServer builds a resource server over testSchema with the fake operation and a stub verifier.
//
// @arg allow Whether the stub verifier allows operations.
// @return *ResourceServer The resource server under test.
// @return *stubVerifier The injected verifier.
//
// @testcase TestOperationAllowed builds a resource server with this helper.
func newResourceServer(allow bool) (*ResourceServer, *stubVerifier) {
	reg := NewRegistry()
	reg.Register("t.read", func(map[string]any) (Operation, error) { return fakeOp{}, nil })
	sv := &stubVerifier{allow: allow}
	g := New(Config{Schema: testSchema(), Registry: reg, ResourceServerID: "rs", Secret: []byte(rsSecret), Verifier: sv})
	return g, sv
}

// readCap is the capability the sign tests use.
//
// @return Capability A repo-scoped read capability.
//
// @testcase TestSignProducesVerifiableRequest signs this capability.
func readCap() Capability {
	return Capability{Actions: []string{"repo.read"}, Resource: ResourceSelector{Type: "t.repo", Match: map[string]string{"owner": "octo", "name": "hello"}}}
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

// TestSchemaHelpers checks HasAction, ResourceEntity and ActionLattice over the schema.
func TestSchemaHelpers(t *testing.T) {
	s := testSchema()
	if !s.HasAction("repo.read") || !s.HasAction("read") || s.HasAction("nope") {
		t.Fatal("HasAction wrong")
	}
	if e, ok := s.ResourceEntity("t.repo"); !ok || e != "Test::Repo" {
		t.Fatalf("ResourceEntity = %q,%v", e, ok)
	}
	if _, ok := s.ResourceEntity("unknown"); ok {
		t.Fatal("unknown resource resolved")
	}
	lat := s.ActionLattice()
	if _, ok := lat["read"]; !ok {
		t.Fatal("group missing from lattice")
	}
	if got := lat["repo.read"]; len(got) != 1 || got[0] != "read" {
		t.Fatalf("action parents = %v", got)
	}
}

// TestRegistry builds a registered operation and errors on an unknown type.
func TestRegistry(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Build(OperationRequest{Type: "missing"}); err == nil {
		t.Fatal("expected error for unknown type")
	}
	reg.Register("t.read", func(map[string]any) (Operation, error) { return fakeOp{}, nil })
	op, err := reg.Build(OperationRequest{Type: "t.read"})
	if err != nil || op == nil {
		t.Fatalf("build: %v", err)
	}
	if types := reg.Types(); len(types) != 1 || types[0] != "t.read" {
		t.Fatalf("types = %v", types)
	}
}

// TestPoliciesFromCapabilities builds a permit and rejects an unknown action.
func TestPoliciesFromCapabilities(t *testing.T) {
	s := testSchema()
	pols, err := policiesFromCapabilities(s, []Capability{readCap()})
	if err != nil || len(pols) != 1 {
		t.Fatalf("policies: %v %v", pols, err)
	}
	for _, want := range []string{`Test::Agent::"agent"`, `Test::Action::"repo.read"`, `Test::Repo::"octo/hello"`} {
		if !bytes.Contains([]byte(pols[0]), []byte(want)) {
			t.Fatalf("policy %q missing %q", pols[0], want)
		}
	}
	bad := Capability{Actions: []string{"bogus"}, Resource: readCap().Resource}
	if _, err := policiesFromCapabilities(s, []Capability{bad}); err == nil {
		t.Fatal("expected unknown-action error")
	}
}

// TestVerifyWorld checks the requests and entity world name the right principal/action/
// resource and include the action lattice and the resource parent chain.
func TestVerifyWorld(t *testing.T) {
	s := testSchema()
	reqs := []Requirement{{Action: "repo.read", Resource: repoRef("octo", "hello")}}

	rq := verifyRequests(s, reqs)
	if len(rq) != 1 || rq[0].Principal.Type != "Test::Agent" || rq[0].Action.ID != "repo.read" || rq[0].Resource.Type != "Test::Repo" {
		t.Fatalf("unexpected request: %+v", rq[0])
	}

	present := map[string]bool{}
	for _, e := range verifyWorld(s, reqs) {
		present[e.Type+"::"+e.ID] = true
	}
	for _, want := range []string{"Test::Agent::agent", "Test::Action::read", "Test::Action::repo.read", "Test::Repo::octo/hello", "Test::Org::octo"} {
		if !present[want] {
			t.Fatalf("world missing %q", want)
		}
	}
}

// TestPresentation renders one grant detail per capability, labelling actions by their
// friendly description rather than the raw name, with the resolved scope.
func TestPresentation(t *testing.T) {
	p := buildPresentation(testSchema(), "", []Capability{readCap()})
	if len(p.Grants) != 1 {
		t.Fatalf("grants = %v", p.Grants)
	}
	g := p.Grants[0]
	// testSchema's repo.read action has description "Read a repo." — the friendly label;
	// the t.repo resource has title "Repo" — the human type name.
	if len(g.Actions) != 1 || g.Actions[0] != "Read a repo." || g.Resource != "octo/hello" || g.ResourceType != "Repo" {
		t.Fatalf("grant detail = %+v", g)
	}

	// A name with no schema entry falls back to the raw name.
	if got := actionLabels(testSchema(), []string{"unknown.x"}); got[0] != "unknown.x" {
		t.Fatalf("fallback label = %q", got[0])
	}
}

// TestExpandTemplate expands a template's scope and condition params into a single permit
// with the right Cedar literals/conditions and a rendered, conditioned presentation.
func TestExpandTemplate(t *testing.T) {
	s := testSchema()
	policies, pres, err := expandTemplate(s, "comment", map[string]string{"owner": "octo", "name": "hello", "label": "bug"})
	if err != nil || len(policies) != 1 {
		t.Fatalf("expand: %v %v", policies, err)
	}
	for _, want := range []string{
		`Test::Agent::"agent"`, `Test::Action::"repo.read"`, `Test::Repo::"octo/hello"`,
		`resource.state == "open"`, `resource.labels.contains("bug")`,
	} {
		if !strings.Contains(policies[0], want) {
			t.Fatalf("policy %q missing %q", policies[0], want)
		}
	}
	if pres.Summary != "Comment on octo/hello" {
		t.Fatalf("summary = %q", pres.Summary)
	}
	if len(pres.Grants) != 1 || pres.Grants[0].Resource != "octo/hello" || pres.Grants[0].ResourceType != "Repo" {
		t.Fatalf("grants = %+v", pres.Grants)
	}
	conds := pres.Grants[0].Conditions
	if len(conds) != 2 || conds[0] != "state is open" || conds[1] != "labels contains bug" {
		t.Fatalf("conditions = %v", conds)
	}

	// Optional condition param omitted → no label condition.
	_, pres2, err := expandTemplate(s, "comment", map[string]string{"owner": "octo", "name": "hello"})
	if err != nil || len(pres2.Grants[0].Conditions) != 1 {
		t.Fatalf("omitted label: %v %+v", err, pres2.Grants[0].Conditions)
	}

	// conditionLiteral operator coverage.
	if _, _, err := conditionLiteral("x", "bogus", "v"); err == nil {
		t.Fatal("expected error for unknown operator")
	}
	if pred, _, _ := conditionLiteral("name", "like", "feature/*"); pred != `resource.name like "feature/*"` {
		t.Fatalf("like predicate = %q", pred)
	}
}

// TestExpandTemplateRequiredParam errors when a required parameter is unbound.
func TestExpandTemplateRequiredParam(t *testing.T) {
	if _, _, err := expandTemplate(testSchema(), "comment", map[string]string{"name": "hello"}); err == nil {
		t.Fatal("expected error for missing required owner")
	}
}

// TestExpandTemplateUnknown errors on an unknown template name.
func TestExpandTemplateUnknown(t *testing.T) {
	if _, _, err := expandTemplate(testSchema(), "nope", nil); err == nil {
		t.Fatal("expected error for unknown template")
	}
}

// TestSignTemplate signs a template instantiation through the sign endpoint.
func TestSignTemplate(t *testing.T) {
	g, _ := newResourceServer(true)
	body, _ := json.Marshal(GrantRequest{Template: "comment", Bindings: map[string]string{"owner": "octo", "name": "hello"}})
	resp := post(t, g.Handler(), "/api/grant-requests/sign", body, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var sgr proposal.SignedGrantRequest
	if err := json.NewDecoder(resp.Body).Decode(&sgr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !sgr.Verify([]byte(rsSecret)) || len(sgr.Policies) != 1 || sgr.Presentation.Summary != "Comment on octo/hello" {
		t.Fatalf("bad signed template: %+v", sgr)
	}
}

// TestSignRejectsBothOrNeither rejects a sign request that sets both forms or neither.
func TestSignRejectsBothOrNeither(t *testing.T) {
	g, _ := newResourceServer(true)
	both, _ := json.Marshal(GrantRequest{Template: "comment", Capabilities: []Capability{readCap()}})
	neither, _ := json.Marshal(GrantRequest{})
	for _, body := range [][]byte{both, neither} {
		resp := post(t, g.Handler(), "/api/grant-requests/sign", body, "")
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	}
}

// TestSchemaServed checks GET /api/schema returns the schema as JSON.
func TestSchemaServed(t *testing.T) {
	g, _ := newResourceServer(true)
	ts := httptest.NewServer(g.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/schema")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var obj map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if obj["resources"] == nil {
		t.Fatal("schema missing resources")
	}
}

// TestSignProducesVerifiableRequest signs a capability bundle and checks it verifies
// under the resource server secret and is attributed to the resource server.
func TestSignProducesVerifiableRequest(t *testing.T) {
	g, _ := newResourceServer(true)
	body, _ := json.Marshal(GrantRequest{Reason: "work", Capabilities: []Capability{readCap()}})
	resp := post(t, g.Handler(), "/api/grant-requests/sign", body, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var sgr proposal.SignedGrantRequest
	if err := json.NewDecoder(resp.Body).Decode(&sgr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if sgr.ResourceServerID != "rs" || !sgr.Verify([]byte(rsSecret)) || len(sgr.Policies) == 0 {
		t.Fatalf("bad signed request: %+v", sgr)
	}
}

// TestSignRejectsUnknownAction rejects a capability naming an action not in the schema.
func TestSignRejectsUnknownAction(t *testing.T) {
	g, _ := newResourceServer(true)
	bad := Capability{Actions: []string{"bogus.action"}, Resource: readCap().Resource}
	body, _ := json.Marshal(GrantRequest{Capabilities: []Capability{bad}})
	resp := post(t, g.Handler(), "/api/grant-requests/sign", body, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestOperationDenied returns 403 when the AS denies.
func TestOperationDenied(t *testing.T) {
	g, _ := newResourceServer(false)
	body, _ := json.Marshal(OperationRequest{Type: "t.read"})
	resp := post(t, g.Handler(), "/api/operations", body, "tok")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

// TestOperationAllowed executes and returns a result when the AS allows, passing the
// bearer token through to the verifier.
func TestOperationAllowed(t *testing.T) {
	g, sv := newResourceServer(true)
	body, _ := json.Marshal(OperationRequest{Type: "t.read"})
	resp := post(t, g.Handler(), "/api/operations", body, "tok")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if sv.gotToken != "tok" {
		t.Fatalf("token = %q", sv.gotToken)
	}
	var out struct {
		Status string         `json:"status"`
		Result map[string]any `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if out.Status != "completed" || out.Result["ok"] != true {
		t.Fatalf("unexpected result: %+v", out)
	}
}
