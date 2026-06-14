package authz

import (
	"testing"

	"github.com/clems4ever/granular/internal/api"
)

func TestAllowsAllDeniesWithoutPolicy(t *testing.T) {
	reqs := []Requirement{{Action: "issue.view", Resource: IssueRef("clems4ever/granular", 7)}}
	ok, err := AllowsAll(nil, Principal(), reqs)
	if err != nil || ok {
		t.Fatalf("no policies must deny, got ok=%v err=%v", ok, err)
	}
}

func TestAllowsAllWithMinimalPermit(t *testing.T) {
	reqs := []Requirement{{Action: "issue.view", Resource: IssueRef("clems4ever/granular", 7)}}
	policies := MinimalPermits(Principal(), reqs)
	ok, err := AllowsAll(policies, Principal(), reqs)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if !ok {
		t.Fatalf("minimal permit should authorize its own requirement\npolicy:\n%s", policies[0])
	}

	// A different issue must NOT be covered by that minimal permit.
	other := []Requirement{{Action: "issue.view", Resource: IssueRef("clems4ever/granular", 8)}}
	if ok, _ := AllowsAll(policies, Principal(), other); ok {
		t.Fatal("minimal permit must not cover a different issue")
	}
}

func TestMinimalPermitContextScopesWrites(t *testing.T) {
	withBody := func(h string) []Requirement {
		return []Requirement{{Action: "issue.comment", Resource: IssueRef("o/n", 1), Context: map[string]string{"body_hash": h}}}
	}
	policies := MinimalPermits(Principal(), withBody("abc"))
	if ok, err := AllowsAll(policies, Principal(), withBody("abc")); err != nil || !ok {
		t.Fatalf("same body should be allowed: ok=%v err=%v", ok, err)
	}
	if ok, _ := AllowsAll(policies, Principal(), withBody("different")); ok {
		t.Fatal("different body must require a fresh approval")
	}
}

func TestPoliciesFromCapabilities(t *testing.T) {
	caps := []api.Capability{
		{Actions: []string{"repo.clone", "issues.read"}, Resource: api.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "clems4ever", "name": "granular"}}},
		{Actions: []string{"issues.read"}, Resource: api.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "clems4ever", "name": "*"}}},
	}
	policies, err := PoliciesFromCapabilities(Principal(), caps)
	if err != nil {
		t.Fatal(err)
	}

	// The repo capability covers a concrete view in that repo.
	view := []Requirement{{Action: "issue.view", Resource: IssueRef("clems4ever/granular", 7)}}
	if ok, _ := AllowsAll(policies, Principal(), view); !ok {
		t.Fatalf("issues.read on repo should cover issue.view\npolicies:\n%v", policies)
	}
	// The org-wide capability covers a different repo under the same owner.
	otherRepo := []Requirement{{Action: "issue.view", Resource: IssueRef("clems4ever/other", 1)}}
	if ok, _ := AllowsAll(policies, Principal(), otherRepo); !ok {
		t.Fatal("org-wide grant should cover another repo under the owner")
	}
	// But not a write.
	write := []Requirement{{Action: "issue.create", Resource: RepoRef("clems4ever/granular")}}
	if ok, _ := AllowsAll(policies, Principal(), write); ok {
		t.Fatal("read capabilities must not authorize a write")
	}
}

func TestPoliciesFromCapabilitiesRejectsUnknownAction(t *testing.T) {
	caps := []api.Capability{{Actions: []string{"issue.delete"}, Resource: api.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "o", "name": "n"}}}}
	if _, err := PoliciesFromCapabilities(Principal(), caps); err == nil {
		t.Fatal("expected error for unknown action")
	}
}
