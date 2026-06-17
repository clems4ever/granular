package authz

import "github.com/clems4ever/granular/internal/verify"

// VerifyRequests turns operation requirements into the generic authorization
// questions a gateway sends to the AS: one verify.Request per requirement, naming the
// fixed agent principal, the action, the resource and any context.
//
// @arg reqs The operation's requirements.
// @return []verify.Request One question per requirement.
//
// @testcase TestVerifyWorldRoundTrips builds requests an AS engine then allows.
func VerifyRequests(reqs []Requirement) []verify.Request {
	out := make([]verify.Request, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, verify.Request{
			Principal: verify.EntityRef{Type: string(TypeAgent), ID: agentID},
			Action:    verify.EntityRef{Type: string(TypeAction), ID: r.Action},
			Resource:  verify.EntityRef{Type: string(resourceEntity(r.Resource.Type)), ID: r.Resource.ID},
			Context:   r.Context,
		})
	}
	return out
}

// VerifyWorld builds the complete Cedar entity world the AS needs to evaluate the
// requirements generically: the agent principal, the full action lattice (so action
// group roll-ups resolve), and every requirement's resource and parent chain (with
// attributes for matcher conditions). The AS receives this as opaque data and never
// needs the catalog itself.
//
// @arg reqs The operation's requirements.
// @return []verify.Entity The entity world (principal, action lattice, resource chains).
//
// @testcase TestVerifyWorldRoundTrips supplies this world to an AS engine.
func VerifyWorld(reqs []Requirement) []verify.Entity {
	seen := map[string]bool{}
	var out []verify.Entity
	add := func(e verify.Entity) {
		key := e.Type + "\x00" + e.ID
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, e)
	}

	// The agent principal.
	add(verify.Entity{Type: string(TypeAgent), ID: agentID})

	// The action lattice: every action/group wired to its parent groups.
	for name, parents := range cat.ActionLattice() {
		parentRefs := make([]verify.EntityRef, 0, len(parents))
		for _, p := range parents {
			parentRefs = append(parentRefs, verify.EntityRef{Type: string(TypeAction), ID: p})
		}
		add(verify.Entity{Type: string(TypeAction), ID: name, Parents: parentRefs})
	}

	// Each requirement's resource chain.
	for _, r := range reqs {
		addResource(r.Resource, add)
	}
	return out
}

// addResource registers a resource reference and its parent chain via add.
//
// @arg ref The resource reference to register.
// @arg add The sink that deduplicates and collects entities.
//
// @testcase TestVerifyWorldRoundTrips registers issue/repo/org chains.
func addResource(ref ResourceRef, add func(verify.Entity)) {
	var parents []verify.EntityRef
	if ref.Parent != nil {
		parents = append(parents, verify.EntityRef{Type: string(resourceEntity(ref.Parent.Type)), ID: ref.Parent.ID})
		addResource(*ref.Parent, add)
	}
	add(verify.Entity{
		Type:    string(resourceEntity(ref.Type)),
		ID:      ref.ID,
		Parents: parents,
		Attrs:   ref.Attrs,
	})
}
