package resourceservergithub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/resourceserver"
)

// TestSchemaDerivedFromCatalog checks the schema mirrors the catalog and carries the
// GitHub entity types and a scope resolver.
func TestSchemaDerivedFromCatalog(t *testing.T) {
	s := Schema()
	if s.AgentType != "GitHub::Agent" || s.ActionType != "GitHub::Action" || s.AgentID != "agent" {
		t.Fatalf("entity types wrong: %+v", s)
	}
	if s.Scope == nil {
		t.Fatal("schema has no scope resolver")
	}
	if !s.HasAction("repo.clone") {
		t.Fatal("schema missing repo.clone")
	}
	if e, ok := s.ResourceEntity("github.repo"); !ok || e != "GitHub::Repo" {
		t.Fatalf("repo entity = %q,%v", e, ok)
	}
}

// TestScopeResolvesRepoAndOrg resolves repo and org scopes and rejects other types.
func TestScopeResolvesRepoAndOrg(t *testing.T) {
	repo := resourceserver.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "octocat", "name": "Hello-World"}}
	et, id, label, err := scope(repo)
	if err != nil || et != "GitHub::Repo" || id != "octocat/Hello-World" || label != "octocat/Hello-World" {
		t.Fatalf("repo scope: %q %q %q %v", et, id, label, err)
	}

	org := resourceserver.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "octocat", "name": "*"}}
	et, id, label, err = scope(org)
	if err != nil || et != "GitHub::Org" || id != "octocat" || !strings.Contains(label, "octocat") {
		t.Fatalf("org scope: %q %q %q %v", et, id, label, err)
	}

	if _, _, _, err := scope(resourceserver.ResourceSelector{Type: "github.issue"}); err == nil {
		t.Fatal("expected unsupported-type error")
	}
}

// TestRegistryBuildsCloneOperation builds an adapted github.clone operation and checks
// its converted requirements, description and executed result.
func TestRegistryBuildsCloneOperation(t *testing.T) {
	reg := Registry("x")
	op, err := reg.Build(resourceserver.OperationRequest{Type: "github.clone", Params: map[string]any{"repo": "octocat/Hello-World"}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "repo.clone" || reqs[0].Resource.Type != "github.repo" {
		t.Fatalf("requirements = %+v", reqs)
	}
	if reqs[0].Resource.Parent == nil || reqs[0].Resource.Parent.Type != "github.org" {
		t.Fatalf("missing org parent: %+v", reqs[0].Resource)
	}
	if !strings.Contains(op.Describe(), "octocat/Hello-World") {
		t.Fatalf("describe = %q", op.Describe())
	}
	res, err := op.Execute(context.Background())
	if err != nil || res["clone_path"] != "/git/octocat/Hello-World.git" {
		t.Fatalf("execute: %v %v", res, err)
	}

	if _, err := reg.Build(resourceserver.OperationRequest{Type: "github.unknown"}); err == nil {
		t.Fatal("expected unknown-type error")
	}
}

// TestTemplatesExpand checks every authored template is exposed in the schema and signs
// into a verifiable grant request with the expected scope and conditions.
func TestTemplatesExpand(t *testing.T) {
	rs := resourceserver.New(resourceserver.Config{
		Schema: Schema(), Registry: Registry("x"),
		ResourceServerID: "github-resource-server", Secret: []byte("s"),
	})

	bind := map[string]map[string]string{
		"read-repo":              {"owner": "clems4ever", "name": "granular"},
		"comment-on-open-issues": {"owner": "clems4ever", "name": "granular", "label": "bug"},
		"triage-issues":          {"owner": "clems4ever", "name": "granular"},
	}
	names := map[string]bool{}
	for _, tpl := range Schema().Templates {
		names[tpl.Name] = true
	}
	for name := range bind {
		if !names[name] {
			t.Fatalf("template %q not exposed in schema", name)
		}
	}

	sign := func(name string, bindings map[string]string) proposal.SignedGrantRequest {
		body, _ := json.Marshal(resourceserver.GrantRequest{Template: name, Bindings: bindings})
		ts := httptest.NewServer(rs.Handler())
		defer ts.Close()
		resp, err := http.Post(ts.URL+"/api/grant-requests/sign", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("template %q sign status %d", name, resp.StatusCode)
		}
		var sgr proposal.SignedGrantRequest
		if err := json.NewDecoder(resp.Body).Decode(&sgr); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !sgr.Verify([]byte("s")) || len(sgr.Policies) != 1 {
			t.Fatalf("template %q: bad signed request %+v", name, sgr)
		}
		return sgr
	}

	for name, bindings := range bind {
		sgr := sign(name, bindings)
		if !strings.Contains(sgr.Policies[0], `GitHub::Repo::"clems4ever/granular"`) {
			t.Fatalf("template %q scope wrong: %s", name, sgr.Policies[0])
		}
	}

	// comment-on-open-issues carries the fixed open-state and label conditions.
	sgr := sign("comment-on-open-issues", bind["comment-on-open-issues"])
	if !strings.Contains(sgr.Policies[0], `resource.state == "open"`) || !strings.Contains(sgr.Policies[0], `resource.labels.contains("bug")`) {
		t.Fatalf("missing conditions: %s", sgr.Policies[0])
	}
	if len(sgr.Presentation.Grants) != 1 || len(sgr.Presentation.Grants[0].Conditions) != 2 {
		t.Fatalf("presentation conditions: %+v", sgr.Presentation.Grants)
	}
}

// TestOperationSpecsCoverRegistry checks there is exactly one operation spec per
// registered operation, and that each spec names a real action and resource.
func TestOperationSpecsCoverRegistry(t *testing.T) {
	specs := operationSpecs()
	registered := map[string]bool{}
	for _, ty := range Registry("").Types() {
		registered[ty] = true
	}
	if len(specs) != len(registered) {
		t.Fatalf("%d specs for %d registered operations", len(specs), len(registered))
	}
	s := Schema()
	for _, op := range specs {
		if !registered[op.Type] {
			t.Fatalf("spec for unregistered operation %q", op.Type)
		}
		if !s.HasAction(op.Action) {
			t.Fatalf("operation %q names unknown action %q", op.Type, op.Action)
		}
		if _, ok := s.ResourceEntity(op.Resource); !ok {
			t.Fatalf("operation %q names unknown resource %q", op.Type, op.Resource)
		}
		if len(op.Params) == 0 {
			t.Fatalf("operation %q has no params", op.Type)
		}
	}
}
