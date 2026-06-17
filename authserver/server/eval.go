package server

import (
	"fmt"
	"strings"

	"github.com/cedar-policy/cedar-go"

	"github.com/clems4ever/granular/internal/verify"
)

// uidOf converts a verify.EntityRef to a Cedar entity uid.
//
// @arg r The wire entity reference.
// @return cedar.EntityUID The Cedar uid.
//
// @testcase TestEvaluateAllowsCoveringPolicy resolves uids from refs.
func uidOf(r verify.EntityRef) cedar.EntityUID {
	return cedar.NewEntityUID(cedar.EntityType(r.Type), cedar.String(r.ID))
}

// evaluate reports whether the opaque policies authorize every request against the
// resource server-supplied entity world. It is fully generic: cedar-go evaluates the
// policies, entities and requests as data, with no knowledge of any platform's
// vocabulary. It returns false (denied) on any request that is not allowed.
//
// @arg policies The opaque Cedar policy texts attached to the token.
// @arg entities The Cedar entity world supplied by the resource server.
// @arg requests The authorization questions; all must be allowed.
// @return bool True when every request is allowed by the policies (and there is at least one).
// @error error when the policy text fails to parse.
//
// @testcase TestEvaluateAllowsCoveringPolicy allows under a covering policy.
// @testcase TestEvaluateDeniesUnrelated denies an unrelated resource.
func evaluate(policies []string, entities []verify.Entity, requests []verify.Request) (bool, error) {
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
			Principal: uidOf(req.Principal),
			Action:    uidOf(req.Action),
			Resource:  uidOf(req.Resource),
			Context:   contextRecord(req.Context),
		})
		if decision != cedar.Allow {
			return false, nil
		}
	}
	return true, nil
}

// buildEntities turns the wire entities into a Cedar entity map.
//
// @arg entities The wire entities supplied by the resource server.
// @return cedar.EntityMap The populated entity map.
//
// @testcase TestProposalApproveFlow builds the world from wire entities.
func buildEntities(entities []verify.Entity) cedar.EntityMap {
	em := cedar.EntityMap{}
	for _, e := range entities {
		uid := cedar.NewEntityUID(cedar.EntityType(e.Type), cedar.String(e.ID))
		parents := make([]cedar.EntityUID, 0, len(e.Parents))
		for _, p := range e.Parents {
			parents = append(parents, uidOf(p))
		}
		em[uid] = cedar.Entity{
			UID:        uid,
			Parents:    cedar.NewEntityUIDSet(parents...),
			Attributes: recordFrom(e.Attrs),
		}
	}
	return em
}

// recordFrom converts a JSON-decoded attribute map into a Cedar record, supporting
// strings and (string) sets; any other scalar is stringified.
//
// @arg m The attribute map (may be nil).
// @return cedar.Record The Cedar record.
//
// @testcase TestProposalApproveFlow passes attributes through.
func recordFrom(m map[string]any) cedar.Record {
	rm := cedar.RecordMap{}
	for k, v := range m {
		rm[cedar.String(k)] = valueFrom(v)
	}
	return cedar.NewRecord(rm)
}

// contextRecord converts a string context map into a Cedar record.
//
// @arg ctx The context map (may be nil).
// @return cedar.Record The Cedar record.
//
// @testcase TestProposalApproveFlow matches a context condition.
func contextRecord(ctx map[string]string) cedar.Record {
	rm := cedar.RecordMap{}
	for k, v := range ctx {
		rm[cedar.String(k)] = cedar.String(v)
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
// @testcase TestProposalApproveFlow converts attribute values.
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
