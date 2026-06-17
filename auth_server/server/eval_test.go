package server

import (
	"testing"

	"github.com/clems4ever/granular/internal/verify"
)

// These tests keep the AS evaluation honest with a deliberately domain-agnostic
// vocabulary (Granular::Agent/Action/Resource/Collection). The AS understands no
// platform's terms — it evaluates whatever opaque world a gateway supplies — so the
// fixtures here are made-up entities, not GitHub ones. A covering policy must allow
// through both an action-group roll-up and a resource hierarchy edge, and a permit
// for one resource must not leak to a sibling.

const (
	agentType  = "Granular::Agent"
	actionType = "Granular::Action"
	resType    = "Granular::Resource"
	collType   = "Granular::Collection"
)

// genericWorld returns the entity world for an agent, an action ("view") nested under
// a group ("read"), and an item parented to a collection — enough to exercise both
// the action lattice and the resource hierarchy.
func genericWorld(item, coll string) []verify.Entity {
	return []verify.Entity{
		{Type: agentType, ID: "agent"},
		{Type: actionType, ID: "view", Parents: []verify.EntityRef{{Type: actionType, ID: "read"}}},
		{Type: actionType, ID: "read"},
		{Type: resType, ID: item, Parents: []verify.EntityRef{{Type: collType, ID: coll}}},
		{Type: collType, ID: coll},
	}
}

// viewRequest asks to "view" the given item as the agent principal.
func viewRequest(item string) []verify.Request {
	return []verify.Request{{
		Principal: verify.EntityRef{Type: agentType, ID: "agent"},
		Action:    verify.EntityRef{Type: actionType, ID: "view"},
		Resource:  verify.EntityRef{Type: resType, ID: item},
	}}
}

// coveringPolicy permits the "read" group on anything under the named collection.
func coveringPolicy(coll string) []string {
	return []string{
		`permit(` +
			`principal == ` + agentType + `::"agent", ` +
			`action in [` + actionType + `::"read"], ` +
			`resource in ` + collType + `::"` + coll + `"` +
			`);`,
	}
}

// TestEvaluateAllowsCoveringPolicy checks a policy scoped to a collection and an
// action group allows a concrete action on an item within that collection — proving
// the AS resolves both the action lattice and the resource hierarchy generically.
func TestEvaluateAllowsCoveringPolicy(t *testing.T) {
	allowed, err := evaluate(coveringPolicy("coll-1"), genericWorld("item-1", "coll-1"), viewRequest("item-1"))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !allowed {
		t.Fatal("denied; the covering policy should allow the action on an item in its collection")
	}
}

// TestEvaluateDeniesUnrelated checks a permit scoped to one collection does not
// authorize an item belonging to a different collection.
func TestEvaluateDeniesUnrelated(t *testing.T) {
	allowed, err := evaluate(coveringPolicy("coll-1"), genericWorld("item-2", "coll-2"), viewRequest("item-2"))
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if allowed {
		t.Fatal("allowed an item outside the granted collection; want denied")
	}
}
