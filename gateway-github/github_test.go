package gatewaygithub

import (
	"context"
	"strings"
	"testing"

	"github.com/clems4ever/granular/gateway"
	"github.com/clems4ever/granular/internal/operations"
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
	repo := gateway.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "octocat", "name": "Hello-World"}}
	et, id, label, err := scope(repo)
	if err != nil || et != "GitHub::Repo" || id != "octocat/Hello-World" || label != "octocat/Hello-World" {
		t.Fatalf("repo scope: %q %q %q %v", et, id, label, err)
	}

	org := gateway.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "octocat", "name": "*"}}
	et, id, label, err = scope(org)
	if err != nil || et != "GitHub::Org" || id != "octocat" || !strings.Contains(label, "octocat") {
		t.Fatalf("org scope: %q %q %q %v", et, id, label, err)
	}

	if _, _, _, err := scope(gateway.ResourceSelector{Type: "github.issue"}); err == nil {
		t.Fatal("expected unsupported-type error")
	}
}

// TestRegistryBuildsCloneOperation builds an adapted github.clone operation and checks
// its converted requirements, description and executed result.
func TestRegistryBuildsCloneOperation(t *testing.T) {
	reg := Registry(operations.Env{BaseURL: "http://gw", GitHubToken: "x"})
	op, err := reg.Build(gateway.OperationRequest{Type: "github.clone", Params: map[string]any{"repo": "octocat/Hello-World"}})
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
	if err != nil || res["clone_url"] == nil {
		t.Fatalf("execute: %v %v", res, err)
	}

	if _, err := reg.Build(gateway.OperationRequest{Type: "github.unknown"}); err == nil {
		t.Fatal("expected unknown-type error")
	}
}
