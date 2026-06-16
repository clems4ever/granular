package server

import (
	"fmt"
	"strings"

	"github.com/cedar-policy/cedar-go"
)

// entityRef identifies a Cedar entity by type and id on the wire.
type entityRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// entityInput is one Cedar entity supplied by the gateway: its uid, hierarchy parents
// and attributes. The AS builds these into the world it evaluates against.
type entityInput struct {
	Type    string         `json:"type"`
	ID      string         `json:"id"`
	Parents []entityRef    `json:"parents,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// requestInput is one authorization question the gateway needs answered: an action by
// a principal on a resource, optionally qualified by context.
type requestInput struct {
	Principal entityRef      `json:"principal"`
	Action    entityRef      `json:"action"`
	Resource  entityRef      `json:"resource"`
	Context   map[string]any `json:"context,omitempty"`
}

// uid converts an entityRef to a Cedar entity uid.
//
// @arg r The wire entity reference.
// @return cedar.EntityUID The Cedar uid.
//
// @testcase TestEvaluateAllowsWithMatchingPolicy resolves uids from refs.
func (r entityRef) uid() cedar.EntityUID {
	return cedar.NewEntityUID(cedar.EntityType(r.Type), cedar.String(r.ID))
}

// evaluate reports whether the opaque policies authorize every request against the
// gateway-supplied entity world. It is fully generic: cedar-go evaluates the
// policies, entities and requests as data, with no knowledge of any platform's
// vocabulary. It returns false (denied) on any request that is not allowed.
//
// @arg policies The opaque Cedar policy texts attached to the token.
// @arg entities The Cedar entity world supplied by the gateway.
// @arg requests The authorization questions; all must be allowed.
// @return bool True when every request is allowed by the policies (and there is at least one).
// @error error when the policy text fails to parse.
//
// @testcase TestEvaluateAllowsWithMatchingPolicy allows under a covering policy.
// @testcase TestEvaluateDeniesWithoutPolicy denies with no policies.
func evaluate(policies []string, entities []entityInput, requests []requestInput) (bool, error) {
	if len(policies) == 0 || len(requests) == 0 {
		return false, nil
	}
	ps, err := cedar.NewPolicySetFromBytes("policy.cedar", []byte(strings.Join(policies, "\n\n")))
	if err != nil {
		return false, err
	}
	em := buildEntities(entities)
	for _, req := range requests {
		decision, _ := cedar.Authorize(ps, em, cedar.Request{
			Principal: req.Principal.uid(),
			Action:    req.Action.uid(),
			Resource:  req.Resource.uid(),
			Context:   recordFrom(req.Context),
		})
		if decision != cedar.Allow {
			return false, nil
		}
	}
	return true, nil
}

// buildEntities turns the wire entities into a Cedar entity map.
//
// @arg entities The wire entities supplied by the gateway.
// @return cedar.EntityMap The populated entity map.
//
// @testcase TestEvaluateAllowsWithMatchingPolicy builds the world from wire entities.
func buildEntities(entities []entityInput) cedar.EntityMap {
	em := cedar.EntityMap{}
	for _, e := range entities {
		uid := cedar.NewEntityUID(cedar.EntityType(e.Type), cedar.String(e.ID))
		parents := make([]cedar.EntityUID, 0, len(e.Parents))
		for _, p := range e.Parents {
			parents = append(parents, p.uid())
		}
		em[uid] = cedar.Entity{
			UID:        uid,
			Parents:    cedar.NewEntityUIDSet(parents...),
			Attributes: recordFrom(e.Attrs),
		}
	}
	return em
}

// recordFrom converts a JSON-decoded attribute/context map into a Cedar record,
// supporting strings, booleans, numbers and (string) sets.
//
// @arg m The attribute map (may be nil).
// @return cedar.Record The Cedar record.
//
// @testcase TestEvaluateAllowsWithMatchingPolicy passes attributes through.
func recordFrom(m map[string]any) cedar.Record {
	rm := cedar.RecordMap{}
	for k, v := range m {
		rm[cedar.String(k)] = valueFrom(v)
	}
	return cedar.NewRecord(rm)
}

// valueFrom converts a single JSON-decoded value into a Cedar value. Strings and
// (string) sets are represented natively; any other scalar is stringified, which
// covers the attribute kinds used for matcher conditions today.
//
// @arg v The decoded value (string or a slice thereof).
// @return cedar.Value The corresponding Cedar value.
//
// @testcase TestEvaluateAllowsWithMatchingPolicy converts attribute values.
func valueFrom(v any) cedar.Value {
	switch x := v.(type) {
	case string:
		return cedar.String(x)
	case []any:
		vals := make([]cedar.Value, 0, len(x))
		for _, e := range x {
			vals = append(vals, valueFrom(e))
		}
		return cedar.NewSet(vals...)
	default:
		return cedar.String(fmt.Sprint(v))
	}
}
