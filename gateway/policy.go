package gateway

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/clems4ever/granular/internal/verify"
)

// policiesFromCapabilities translates a client's capability bundle into Cedar policies,
// validating each action against the schema and resolving each resource selector to a
// scope with the schema's ScopeFunc. The gateway signs these so a client cannot tamper
// with them.
//
// @arg s The schema supplying the action vocabulary, entity types and scope resolver.
// @arg caps The requested capabilities.
// @return []string The generated Cedar policy texts.
// @error error when no capabilities are given, an action is unknown, or scope resolution fails.
//
// @testcase TestPoliciesFromCapabilities builds a policy and rejects an unknown action.
func policiesFromCapabilities(s Schema, caps []Capability) ([]string, error) {
	if len(caps) == 0 {
		return nil, fmt.Errorf("no capabilities requested")
	}
	policies := make([]string, 0, len(caps))
	for _, c := range caps {
		if len(c.Actions) == 0 {
			return nil, fmt.Errorf("capability requires at least one action")
		}
		lits := make([]string, 0, len(c.Actions))
		for _, a := range c.Actions {
			if !s.HasAction(a) {
				return nil, fmt.Errorf("unknown action %q", a)
			}
			lits = append(lits, entityLiteral(s.ActionType, a))
		}
		if s.Scope == nil {
			return nil, fmt.Errorf("schema has no scope resolver")
		}
		et, id, _, err := s.Scope(c.Resource)
		if err != nil {
			return nil, err
		}
		policies = append(policies, fmt.Sprintf(
			"permit (\n  principal == %s,\n  action in [%s],\n  resource in %s\n);",
			entityLiteral(s.AgentType, s.AgentID), strings.Join(lits, ", "), entityLiteral(et, id)))
	}
	return policies, nil
}

// verifyRequests turns operation requirements into the generic authorization questions
// the gateway sends to the AS: one verify.Request per requirement, naming the agent
// principal, the action, the resolved resource entity and any context.
//
// @arg s The schema supplying the principal and resource entity types.
// @arg reqs The operation's requirements.
// @return []verify.Request One question per requirement.
//
// @testcase TestVerifyWorld builds requests naming the right principal/action/resource.
func verifyRequests(s Schema, reqs []Requirement) []verify.Request {
	out := make([]verify.Request, 0, len(reqs))
	for _, r := range reqs {
		et, _ := s.ResourceEntity(r.Resource.Type)
		out = append(out, verify.Request{
			Principal: verify.EntityRef{Type: s.AgentType, ID: s.AgentID},
			Action:    verify.EntityRef{Type: s.ActionType, ID: r.Action},
			Resource:  verify.EntityRef{Type: et, ID: r.Resource.ID},
			Context:   r.Context,
		})
	}
	return out
}

// verifyWorld builds the complete Cedar entity world the AS needs to evaluate the
// requirements generically: the agent principal, the full action lattice (so group
// roll-ups resolve) and every requirement's resource and parent chain. The AS receives
// this as opaque data and never needs the schema itself.
//
// @arg s The schema supplying the entity types and action lattice.
// @arg reqs The operation's requirements.
// @return []verify.Entity The entity world (principal, action lattice, resource chains).
//
// @testcase TestVerifyWorld includes the action lattice and the resource parent chain.
func verifyWorld(s Schema, reqs []Requirement) []verify.Entity {
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

	add(verify.Entity{Type: s.AgentType, ID: s.AgentID})

	for name, parents := range s.ActionLattice() {
		refs := make([]verify.EntityRef, 0, len(parents))
		for _, p := range parents {
			refs = append(refs, verify.EntityRef{Type: s.ActionType, ID: p})
		}
		add(verify.Entity{Type: s.ActionType, ID: name, Parents: refs})
	}

	for _, r := range reqs {
		addResource(s, r.Resource, add)
	}
	return out
}

// addResource registers a resource reference and its parent chain via add, resolving
// each type to its Cedar entity with the schema.
//
// @arg s The schema resolving resource types to entity types.
// @arg ref The resource reference to register.
// @arg add The sink that deduplicates and collects entities.
//
// @testcase TestVerifyWorld registers a resource and its parents.
func addResource(s Schema, ref ResourceRef, add func(verify.Entity)) {
	et, _ := s.ResourceEntity(ref.Type)
	var parents []verify.EntityRef
	if ref.Parent != nil {
		pet, _ := s.ResourceEntity(ref.Parent.Type)
		parents = append(parents, verify.EntityRef{Type: pet, ID: ref.Parent.ID})
		addResource(s, *ref.Parent, add)
	}
	add(verify.Entity{Type: et, ID: ref.ID, Parents: parents, Attrs: ref.Attrs})
}

// entityLiteral renders a typed Cedar entity literal with a quoted id, e.g.
// GitHub::Repo::"owner/name".
//
// @arg typ The Cedar entity type.
// @arg id The entity id.
// @return string The Cedar entity literal.
//
// @testcase TestPoliciesFromCapabilities renders principal, action and resource literals.
func entityLiteral(typ, id string) string {
	return typ + "::" + strconv.Quote(id)
}
