// Package authz is a playground around cedar-go for modelling granular,
// human-approved permissions as Cedar policies. It provides a World entity
// builder (with the GitHub verb-lattice action groups pre-seeded) and a thin
// Engine wrapper over a Cedar PolicySet, so tests can express the kinds of rules
// the project needs and assert how they evaluate.
package authz

import (
	"fmt"
	"strings"

	"github.com/cedar-policy/cedar-go"

	"github.com/clems4ever/granular/gateway-github/internal/catalog"
)

// cat is the single source of truth (the capability catalog) the Cedar layer
// derives its action lattice and resource entity types from.
var cat = catalog.Build()

// The Cedar entity types. Agent and Action are policy-engine concepts; the
// resource types are taken from the catalog so there is one definition of each.
var (
	TypeAgent   = cedar.EntityType("GitHub::Agent")
	TypeAction  = cedar.EntityType("GitHub::Action")
	TypeOrg     = resourceEntity("github.org")
	TypeRepo    = resourceEntity("github.repo")
	TypeIssue   = resourceEntity("github.issue")
	TypeComment = resourceEntity("github.comment")
	TypePull    = resourceEntity("github.pull")
	TypeBranch  = resourceEntity("github.branch")
)

// resourceEntity returns the Cedar entity type for a catalog resource name,
// panicking if it is unknown (a programming error).
//
// @arg name The catalog resource name, e.g. "github.repo".
// @return cedar.EntityType The resource's Cedar entity type, e.g. GitHub::Repo.
//
// @testcase TestReadRollupCoversListAndView uses these entity types as resources.
func resourceEntity(name string) cedar.EntityType {
	for _, r := range cat.Resources {
		if r.Name == name {
			return cedar.EntityType(r.Entity)
		}
	}
	panic("authz: unknown catalog resource " + name)
}

// World accumulates the Cedar entities (resources, principals, and the action
// lattice) needed to evaluate authorization requests.
type World struct {
	entities cedar.EntityMap
}

// NewWorld creates a World pre-seeded with the action lattice.
//
// @return *World A ready-to-use world containing the verb-lattice action entities.
//
// @testcase TestReadRollupCoversListAndView builds a world and authorizes against it.
func NewWorld() *World {
	w := &World{entities: cedar.EntityMap{}}
	seedActions(w.entities)
	return w
}

// Entities returns the accumulated entity map for use in authorization.
//
// @return cedar.EntityMap The entities registered so far.
//
// @testcase TestReadRollupCoversListAndView passes these entities to the engine.
func (w *World) Entities() cedar.EntityMap {
	return w.entities
}

// add registers an entity (idempotently) with the given parents and attributes
// and returns its UID.
//
// @arg uid The entity's unique id.
// @arg parents The entity's direct parents (hierarchy edges).
// @arg attrs The entity's attributes.
// @return cedar.EntityUID The registered entity's uid.
//
// @testcase TestReadOnlyOpenBugIssues relies on attributes added here.
func (w *World) add(uid cedar.EntityUID, parents []cedar.EntityUID, attrs cedar.RecordMap) cedar.EntityUID {
	if _, ok := w.entities[uid]; ok {
		return uid
	}
	w.entities[uid] = cedar.Entity{
		UID:        uid,
		Parents:    cedar.NewEntityUIDSet(parents...),
		Attributes: cedar.NewRecord(attrs),
	}
	return uid
}

// Agent registers and returns the principal (the LLM session) uid.
//
// @arg id The agent/session identifier.
// @return cedar.EntityUID The agent uid.
//
// @testcase TestReadRollupCoversListAndView uses the agent as principal.
func (w *World) Agent(id string) cedar.EntityUID {
	return w.add(cedar.NewEntityUID(TypeAgent, cedar.String(id)), nil, nil)
}

// Org registers and returns an organization uid (the parent of its repos, used to
// express "all repos under this org").
//
// @arg name The organization (owner) login.
// @return cedar.EntityUID The org uid.
//
// @testcase TestOrgWideReadViaHierarchy scopes a grant to an org.
func (w *World) Org(name string) cedar.EntityUID {
	return w.add(cedar.NewEntityUID(TypeOrg, cedar.String(name)), nil, nil)
}

// Repo registers and returns a repository uid, parented to its org.
//
// @arg full The "owner/name" repository.
// @return cedar.EntityUID The repo uid.
//
// @testcase TestRepoScopedIssuesReadCoversBothAndExcludesPRs uses the repo as a resource.
func (w *World) Repo(full string) cedar.EntityUID {
	owner, _, _ := strings.Cut(full, "/")
	org := w.Org(owner)
	return w.add(cedar.NewEntityUID(TypeRepo, cedar.String(full)), []cedar.EntityUID{org}, nil)
}

// Issue registers and returns an issue uid, parented to its repo, carrying its
// state and labels as attributes for matcher conditions.
//
// @arg repoFull The "owner/name" repository.
// @arg number The issue number.
// @arg state The issue state ("open" or "closed").
// @arg labels The issue's labels.
// @return cedar.EntityUID The issue uid.
//
// @testcase TestReadOnlyOpenBugIssues matches on issue state and labels.
func (w *World) Issue(repoFull string, number int, state string, labels ...string) cedar.EntityUID {
	repo := w.Repo(repoFull)
	values := make([]cedar.Value, len(labels))
	for i, l := range labels {
		values[i] = cedar.String(l)
	}
	uid := cedar.NewEntityUID(TypeIssue, cedar.String(fmt.Sprintf("%s#%d", repoFull, number)))
	return w.add(uid, []cedar.EntityUID{repo}, cedar.RecordMap{
		"state":  cedar.String(state),
		"labels": cedar.NewSet(values...),
	})
}

// Comment registers and returns a comment uid, parented to its issue (and thus
// transitively to the repo).
//
// @arg issue The parent issue uid.
// @arg id The comment identifier.
// @return cedar.EntityUID The comment uid.
//
// @testcase TestCommentsRequireSeparateCapability reads a comment resource.
func (w *World) Comment(issue cedar.EntityUID, id int) cedar.EntityUID {
	uid := cedar.NewEntityUID(TypeComment, cedar.String(fmt.Sprintf("%s/comment/%d", string(issue.ID), id)))
	return w.add(uid, []cedar.EntityUID{issue}, nil)
}

// Pull registers and returns a pull-request uid, parented to its repo.
//
// @arg repoFull The "owner/name" repository.
// @arg number The pull-request number.
// @arg state The pull-request state.
// @return cedar.EntityUID The pull-request uid.
//
// @testcase TestRwOnPullRequests reads and writes a pull request.
func (w *World) Pull(repoFull string, number int, state string) cedar.EntityUID {
	repo := w.Repo(repoFull)
	uid := cedar.NewEntityUID(TypePull, cedar.String(fmt.Sprintf("%s!%d", repoFull, number)))
	return w.add(uid, []cedar.EntityUID{repo}, cedar.RecordMap{"state": cedar.String(state)})
}

// Branch registers and returns a branch uid, parented to its repo, carrying its
// name for matcher conditions.
//
// @arg repoFull The "owner/name" repository.
// @arg name The branch name.
// @return cedar.EntityUID The branch uid.
//
// @testcase TestPushOnlyToFeatureBranches matches on the branch name.
func (w *World) Branch(repoFull, name string) cedar.EntityUID {
	repo := w.Repo(repoFull)
	uid := cedar.NewEntityUID(TypeBranch, cedar.String(repoFull+":"+name))
	return w.add(uid, []cedar.EntityUID{repo}, cedar.RecordMap{"name": cedar.String(name)})
}

// Action returns the uid of a (concrete or group) action.
//
// @arg name The action name, e.g. "issue.view" or "read".
// @return cedar.EntityUID The action uid.
//
// @testcase TestReadRollupCoversListAndView requests concrete actions by name.
func Action(name string) cedar.EntityUID {
	return cedar.NewEntityUID(TypeAction, cedar.String(name))
}

// seedActions populates the entity map with the action lattice (derived from the
// catalog), wiring each action and group to its parent groups.
//
// @arg m The entity map to populate.
//
// @testcase TestReadRollupCoversListAndView depends on the seeded action groups.
func seedActions(m cedar.EntityMap) {
	for name, parents := range cat.ActionLattice() {
		parentUIDs := make([]cedar.EntityUID, 0, len(parents))
		for _, p := range parents {
			parentUIDs = append(parentUIDs, Action(p))
		}
		uid := Action(name)
		m[uid] = cedar.Entity{
			UID:        uid,
			Parents:    cedar.NewEntityUIDSet(parentUIDs...),
			Attributes: cedar.NewRecord(nil),
		}
	}
}

// Engine evaluates authorization requests against a Cedar policy set.
type Engine struct {
	ps *cedar.PolicySet
}

// NewEngine parses Cedar policy text into an Engine.
//
// @arg policies The Cedar policy document.
// @return *Engine An engine over the parsed policy set.
// @error error when the policy text fails to parse.
//
// @testcase TestReadRollupCoversListAndView builds an engine from policy text.
func NewEngine(policies string) (*Engine, error) {
	ps, err := cedar.NewPolicySetFromBytes("policies.cedar", []byte(policies))
	if err != nil {
		return nil, err
	}
	return &Engine{ps: ps}, nil
}

// Allowed reports whether the principal may perform the action on the resource in
// the given world.
//
// @arg world The world supplying the entities (resources, principal, actions).
// @arg principal The principal uid.
// @arg action The action uid.
// @arg resource The resource uid.
// @return bool True when the policy set authorizes the request.
//
// @testcase TestReadRollupCoversListAndView asserts allow/deny outcomes.
func (e *Engine) Allowed(world *World, principal, action, resource cedar.EntityUID) bool {
	decision, _ := cedar.Authorize(e.ps, world.Entities(), cedar.Request{
		Principal: principal,
		Action:    action,
		Resource:  resource,
		Context:   cedar.NewRecord(nil),
	})
	return decision == cedar.Allow
}
