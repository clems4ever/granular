package authz

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cedar-policy/cedar-go"

	"github.com/clems4ever/granular/internal/api"
)

// agentID is the single agent principal identity (per-agent identity is future
// work).
const agentID = "agent"

// ResourceRef is an operation-supplied description of a resource being acted on:
// its catalog type, its identity, optional matcher attributes, and its parent in
// the hierarchy. The authz layer turns it into Cedar entities.
type ResourceRef struct {
	Type   string
	ID     string
	Attrs  map[string]any
	Parent *ResourceRef
}

// Requirement is one authorization check an operation needs to pass: an action on
// a resource, optionally qualified by context (e.g. a content hash for writes).
type Requirement struct {
	Action   string
	Resource ResourceRef
	Context  map[string]string
}

// Principal returns the agent principal uid used for authorization and in
// generated policies.
//
// @return cedar.EntityUID The agent principal.
//
// @testcase TestAllowsAllWithMinimalPermit authorizes against this principal.
func Principal() cedar.EntityUID {
	return cedar.NewEntityUID(TypeAgent, agentID)
}

// OrgRef builds a resource reference for an organization (owner).
//
// @arg owner The owner login.
// @return ResourceRef The org reference.
//
// @testcase TestAllowsAllWithMinimalPermit builds refs via these constructors.
func OrgRef(owner string) ResourceRef {
	return ResourceRef{Type: "github.org", ID: owner}
}

// RepoRef builds a resource reference for a repository, parented to its org.
//
// @arg full The "owner/name" repository.
// @return ResourceRef The repo reference.
//
// @testcase TestAllowsAllWithMinimalPermit builds a repo requirement.
func RepoRef(full string) ResourceRef {
	owner, _, _ := strings.Cut(full, "/")
	org := OrgRef(owner)
	return ResourceRef{Type: "github.repo", ID: full, Parent: &org}
}

// IssueRef builds a resource reference for an issue, parented to its repo.
//
// @arg full The "owner/name" repository.
// @arg number The issue number.
// @return ResourceRef The issue reference.
//
// @testcase TestAllowsAllWithMinimalPermit builds an issue requirement.
func IssueRef(full string, number int) ResourceRef {
	repo := RepoRef(full)
	return ResourceRef{Type: "github.issue", ID: fmt.Sprintf("%s#%d", full, number), Parent: &repo}
}

// PullRef builds a resource reference for a pull request, parented to its repo.
//
// @arg full The "owner/name" repository.
// @arg number The pull request number.
// @return ResourceRef The pull request reference.
//
// @testcase TestPullAndBranchRefs builds a pull requirement parented to its repo.
func PullRef(full string, number int) ResourceRef {
	repo := RepoRef(full)
	return ResourceRef{Type: "github.pull", ID: fmt.Sprintf("%s#%d", full, number), Parent: &repo}
}

// BranchRef builds a resource reference for a branch, parented to its repo.
//
// @arg full The "owner/name" repository.
// @arg branch The branch name.
// @return ResourceRef The branch reference.
//
// @testcase TestPullAndBranchRefs builds a branch requirement parented to its repo.
func BranchRef(full, branch string) ResourceRef {
	repo := RepoRef(full)
	return ResourceRef{Type: "github.branch", ID: full + ":" + branch, Parent: &repo}
}

// AllowsAll reports whether the active policy set authorizes every requirement for
// the principal.
//
// @arg policies The active (non-expired) Cedar policy texts.
// @arg principal The principal uid.
// @arg reqs The requirements that must all be allowed.
// @return bool True when every requirement is allowed (and there is at least one).
// @error error when the policy text fails to parse.
//
// @testcase TestAllowsAllWithMinimalPermit allows after minting a minimal permit.
// @testcase TestAllowsAllDeniesWithoutPolicy denies with no policies.
func AllowsAll(policies []string, principal cedar.EntityUID, reqs []Requirement) (bool, error) {
	if len(policies) == 0 || len(reqs) == 0 {
		return false, nil
	}
	ps, err := cedar.NewPolicySetFromBytes("active.cedar", []byte(strings.Join(policies, "\n\n")))
	if err != nil {
		return false, err
	}
	entities := entitiesFor(principal, reqs)
	for _, r := range reqs {
		decision, _ := cedar.Authorize(ps, entities, cedar.Request{
			Principal: principal,
			Action:    Action(r.Action),
			Resource:  refUID(r.Resource),
			Context:   contextRecord(r.Context),
		})
		if decision != cedar.Allow {
			return false, nil
		}
	}
	return true, nil
}

// MinimalPermits returns one minimal Cedar permit per requirement: it authorizes
// exactly that action on that exact resource (and context), so approving it grants
// no more than the operation needs.
//
// @arg principal The principal the permits are written for.
// @arg reqs The requirements to turn into permits.
// @return []string The generated Cedar policy texts.
//
// @testcase TestAllowsAllWithMinimalPermit feeds these back into AllowsAll.
func MinimalPermits(principal cedar.EntityUID, reqs []Requirement) []string {
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, minimalPermit(principal, r))
	}
	return out
}

// PoliciesFromCapabilities translates a custom permissions request into Cedar
// policies, validating action and resource names against the catalog.
//
// @arg principal The principal the policies are written for.
// @arg caps The requested capabilities.
// @return []string The generated Cedar policy texts.
// @error error when a capability names an unknown action or unsupported resource.
//
// @testcase TestPoliciesFromCapabilities builds repo- and org-scoped policies.
func PoliciesFromCapabilities(principal cedar.EntityUID, caps []api.Capability) ([]string, error) {
	if len(caps) == 0 {
		return nil, fmt.Errorf("no capabilities requested")
	}
	policies := make([]string, 0, len(caps))
	for _, c := range caps {
		actions, err := actionListLiteral(c.Actions)
		if err != nil {
			return nil, err
		}
		scope, err := scopeLiteral(c.Resource)
		if err != nil {
			return nil, err
		}
		policies = append(policies, fmt.Sprintf(
			"permit (\n  principal == %s,\n  action in %s,\n  resource in %s\n);",
			uidLiteral(principal), actions, scope))
	}
	return policies, nil
}

// entitiesFor builds the entity map (action lattice, principal, and the resource
// chains of every requirement) needed to evaluate the requirements.
//
// @arg principal The principal uid.
// @arg reqs The requirements whose resources must be present.
// @return cedar.EntityMap The populated entity map.
//
// @testcase TestAllowsAllWithMinimalPermit evaluates against these entities.
func entitiesFor(principal cedar.EntityUID, reqs []Requirement) cedar.EntityMap {
	m := cedar.EntityMap{}
	seedActions(m)
	m[principal] = cedar.Entity{UID: principal, Parents: cedar.NewEntityUIDSet(), Attributes: cedar.NewRecord(nil)}
	for _, r := range reqs {
		addRef(m, r.Resource)
	}
	return m
}

// addRef registers a resource reference and its parent chain in the entity map and
// returns its uid.
//
// @arg m The entity map to populate.
// @arg ref The resource reference to register.
// @return cedar.EntityUID The reference's uid.
//
// @testcase TestAllowsAllWithMinimalPermit registers issue/repo/org chains.
func addRef(m cedar.EntityMap, ref ResourceRef) cedar.EntityUID {
	var parents []cedar.EntityUID
	if ref.Parent != nil {
		parents = append(parents, addRef(m, *ref.Parent))
	}
	uid := refUID(ref)
	m[uid] = cedar.Entity{UID: uid, Parents: cedar.NewEntityUIDSet(parents...), Attributes: recordFromAttrs(ref.Attrs)}
	return uid
}

// refUID returns the Cedar uid of a resource reference.
//
// @arg ref The resource reference.
// @return cedar.EntityUID The reference's uid.
//
// @testcase TestAllowsAllWithMinimalPermit resolves resource uids.
func refUID(ref ResourceRef) cedar.EntityUID {
	return cedar.NewEntityUID(resourceEntity(ref.Type), cedar.String(ref.ID))
}

// recordFromAttrs converts attribute values (string or []string) into a Cedar
// record.
//
// @arg attrs The attribute map (may be nil).
// @return cedar.Record The Cedar record.
//
// @testcase TestAllowsAllWithMinimalPermit passes empty attributes.
func recordFromAttrs(attrs map[string]any) cedar.Record {
	rm := cedar.RecordMap{}
	for k, v := range attrs {
		switch x := v.(type) {
		case string:
			rm[cedar.String(k)] = cedar.String(x)
		case []string:
			vals := make([]cedar.Value, len(x))
			for i, s := range x {
				vals[i] = cedar.String(s)
			}
			rm[cedar.String(k)] = cedar.NewSet(vals...)
		default:
			rm[cedar.String(k)] = cedar.String(fmt.Sprint(v))
		}
	}
	return cedar.NewRecord(rm)
}

// contextRecord converts a string context map into a Cedar record.
//
// @arg ctx The context map (may be nil).
// @return cedar.Record The Cedar record.
//
// @testcase TestAllowsAllWithMinimalPermit matches a context condition.
func contextRecord(ctx map[string]string) cedar.Record {
	rm := cedar.RecordMap{}
	for k, v := range ctx {
		rm[cedar.String(k)] = cedar.String(v)
	}
	return cedar.NewRecord(rm)
}

// minimalPermit renders a single minimal permit for a requirement.
//
// @arg principal The principal the permit is written for.
// @arg r The requirement.
// @return string The Cedar permit text.
//
// @testcase TestAllowsAllWithMinimalPermit parses and evaluates this output.
func minimalPermit(principal cedar.EntityUID, r Requirement) string {
	resType := string(resourceEntity(r.Resource.Type))
	var b strings.Builder
	fmt.Fprintf(&b, "permit (\n  principal == %s,\n  action == %s,\n  resource == %s\n)",
		uidLiteral(principal), actionLiteral(r.Action), entityLiteral(resType, r.Resource.ID))
	if len(r.Context) > 0 {
		keys := make([]string, 0, len(r.Context))
		for k := range r.Context {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		conds := make([]string, 0, len(keys))
		for _, k := range keys {
			conds = append(conds, fmt.Sprintf("context.%s == %s", k, strconv.Quote(r.Context[k])))
		}
		fmt.Fprintf(&b, " when { %s }", strings.Join(conds, " && "))
	}
	b.WriteString(";")
	return b.String()
}

// scopeLiteral renders the Cedar resource scope for a selector (repo, or org when
// the name is "*"/empty).
//
// @arg sel The resource selector.
// @return string The Cedar entity literal to scope `resource in` to.
// @error error when the type is unsupported or the owner is missing.
//
// @testcase TestPoliciesFromCapabilities exercises repo and org scopes.
func scopeLiteral(sel api.ResourceSelector) (string, error) {
	if sel.Type != "github.repo" {
		return "", fmt.Errorf("unsupported resource type %q (only github.repo for now)", sel.Type)
	}
	owner := sel.Match["owner"]
	if owner == "" {
		return "", fmt.Errorf("resource match requires an owner")
	}
	if name := sel.Match["name"]; name != "" && name != "*" {
		return entityLiteral("GitHub::Repo", owner+"/"+name), nil
	}
	return entityLiteral("GitHub::Org", owner), nil
}

// actionListLiteral renders a Cedar action list, validating each name against the
// catalog.
//
// @arg names The action or group names.
// @return string The Cedar action list literal.
// @error error when no actions are given or a name is unknown.
//
// @testcase TestPoliciesFromCapabilities rejects unknown actions.
func actionListLiteral(names []string) (string, error) {
	if len(names) == 0 {
		return "", fmt.Errorf("capability requires at least one action")
	}
	lits := make([]string, 0, len(names))
	for _, n := range names {
		if !cat.HasAction(n) {
			return "", fmt.Errorf("unknown action %q", n)
		}
		lits = append(lits, actionLiteral(n))
	}
	return "[" + strings.Join(lits, ", ") + "]", nil
}

// uidLiteral renders an entity uid as a Cedar literal.
//
// @arg uid The entity uid.
// @return string The Cedar entity literal.
//
// @testcase TestAllowsAllWithMinimalPermit renders the principal literal.
func uidLiteral(uid cedar.EntityUID) string {
	return entityLiteral(string(uid.Type), string(uid.ID))
}

// actionLiteral renders an action name as a Cedar GitHub::Action literal.
//
// @arg name The action name.
// @return string The Cedar action literal.
//
// @testcase TestAllowsAllWithMinimalPermit renders action literals.
func actionLiteral(name string) string {
	return entityLiteral("GitHub::Action", name)
}

// entityLiteral renders a typed entity literal with a quoted id.
//
// @arg typ The Cedar entity type, e.g. "GitHub::Repo".
// @arg id The entity id.
// @return string The Cedar entity literal, e.g. GitHub::Repo::"owner/name".
//
// @testcase TestPoliciesFromCapabilities renders resource literals.
func entityLiteral(typ, id string) string {
	return typ + "::" + strconv.Quote(id)
}
