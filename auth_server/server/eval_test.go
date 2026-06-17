package server

import (
	"testing"

	"github.com/clems4ever/granular/internal/authz"
)

// TestEvaluateWithAuthzWorld proves the gateway↔AS contract end to end: the generic
// entity world and requests the gateway derives (authz.VerifyWorld/VerifyRequests),
// evaluated against the minimal permits an approval would store, allow the operation.
func TestEvaluateWithAuthzWorld(t *testing.T) {
	reqs := []authz.Requirement{{Action: "issue.view", Resource: authz.IssueRef("octocat/Hello-World", 1)}}
	policies := authz.MinimalPermits(authz.Principal(), reqs)

	allowed, err := evaluate(policies, authz.VerifyWorld(reqs), authz.VerifyRequests(reqs))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !allowed {
		t.Fatal("denied; the derived world/requests should be allowed by their own minimal permits")
	}
}

// TestEvaluateDeniesUnrelated checks a permit for one issue does not authorize another.
func TestEvaluateDeniesUnrelated(t *testing.T) {
	granted := []authz.Requirement{{Action: "issue.view", Resource: authz.IssueRef("octocat/Hello-World", 1)}}
	policies := authz.MinimalPermits(authz.Principal(), granted)

	asked := []authz.Requirement{{Action: "issue.view", Resource: authz.IssueRef("octocat/Hello-World", 2)}}
	allowed, err := evaluate(policies, authz.VerifyWorld(asked), authz.VerifyRequests(asked))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if allowed {
		t.Fatal("allowed an unrelated issue; want denied")
	}
}
