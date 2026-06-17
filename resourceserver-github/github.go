// Package resourceservergithub is the concrete GitHub resource server built on the generic resource server
// SDK. It supplies the three domain-specific things the SDK needs — the GitHub
// permission Schema (derived from the capability catalog), the scope resolver that maps
// a capability selector to a Cedar GitHub entity, and the GitHub action implementations
// (adapted from resourceserver-github/internal/operations/github) wired into a Registry. All
// GitHub-specific concerns — the catalog vocabulary, the Cedar entity world, and the
// operation implementations — live under resourceserver-github/internal, so nothing outside
// this resource server can import them. Anyone wanting a different platform writes a sibling of
// this package against the same SDK.
package resourceservergithub

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/resourceserver"
	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
	"github.com/clems4ever/granular/resourceserver-github/internal/catalog"
	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
	githubops "github.com/clems4ever/granular/resourceserver-github/internal/operations/github"
)

// Schema returns the GitHub permission vocabulary for the resource server SDK, derived from the
// capability catalog (the single source of resources, groups and actions) and tagged
// with the GitHub Cedar entity types and the GitHub scope resolver.
//
// @return resourceserver.Schema The GitHub schema the resource server exposes.
//
// @testcase TestSchemaDerivedFromCatalog mirrors the catalog and carries a scope resolver.
func Schema() resourceserver.Schema {
	cat := catalog.Build()
	s := resourceserver.Schema{
		AgentType:  "GitHub::Agent",
		ActionType: "GitHub::Action",
		AgentID:    "agent",
		Example:    cat.RequestExample,
		Scope:      scope,
	}
	for _, r := range cat.Resources {
		match := make([]resourceserver.MatchField, len(r.Match))
		for i, m := range r.Match {
			match[i] = resourceserver.MatchField{Name: m.Name, Type: m.Type, Description: m.Description}
		}
		s.Resources = append(s.Resources, resourceserver.ResourceType{
			Name: r.Name, Title: r.Title, Entity: r.Entity, Parent: r.Parent,
			Description: r.Description, Match: match,
		})
	}
	for _, g := range cat.Groups {
		s.Groups = append(s.Groups, resourceserver.Group{Name: g.Name, Title: g.Title, Description: g.Description, Parents: g.Parents})
	}
	for _, a := range cat.Actions {
		s.Actions = append(s.Actions, resourceserver.Action{Name: a.Name, Title: a.Title, Resource: a.Resource, Groups: a.Groups, Description: a.Description})
	}
	s.Operations = operationSpecs()
	s.Templates = templates()
	return s
}

// scope maps a capability's resource selector to the Cedar GitHub entity its permit is
// scoped to (a repository, or its owning organization when the name is wildcarded) and a
// human-readable label.
//
// @arg sel The resource selector from a capability.
// @return string The Cedar entity type, e.g. "GitHub::Repo" or "GitHub::Org".
// @return string The Cedar entity id.
// @return string A human-readable scope label.
// @error error when the resource type is unsupported or the owner is missing.
//
// @testcase TestScopeResolvesRepoAndOrg resolves repo and org scopes and rejects others.
func scope(sel resourceserver.ResourceSelector) (string, string, string, error) {
	if sel.Type != "github.repo" {
		return "", "", "", fmt.Errorf("unsupported resource type %q (only github.repo for now)", sel.Type)
	}
	owner := sel.Match["owner"]
	if owner == "" {
		return "", "", "", fmt.Errorf("resource match requires an owner")
	}
	if name := sel.Match["name"]; name != "" && name != "*" {
		full := owner + "/" + name
		return "GitHub::Repo", full, full, nil
	}
	return "GitHub::Org", owner, "all repositories under " + owner, nil
}

// Registry builds the SDK operation registry for all GitHub actions, binding each
// factory to the execution environment built from the GitHub token. Taking a
// primitive (rather than an operations.Env) keeps the GitHub operation packages
// internal to this resource server.
//
// @arg githubToken The GitHub personal access token operations authenticate with.
// @return *resourceserver.Registry A registry with every GitHub operation registered.
//
// @testcase TestRegistryBuildsCloneOperation builds a github.clone operation.
func Registry(githubToken string) *resourceserver.Registry {
	env := operations.Env{GitHubToken: githubToken}
	reg := resourceserver.NewRegistry()
	register := func(opType string, factory operations.Factory) {
		reg.Register(opType, adapt(factory, env))
	}
	register(githubops.TypeClone, githubops.Clone)
	register(githubops.TypeIssueList, githubops.IssueList)
	register(githubops.TypeIssueView, githubops.IssueView)
	register(githubops.TypeIssueComment, githubops.IssueComment)
	register(githubops.TypeIssueCreate, githubops.IssueCreate)
	register(githubops.TypeIssueEdit, githubops.IssueEdit)
	register(githubops.TypeIssueClose, githubops.IssueClose)
	register(githubops.TypeIssueReopen, githubops.IssueReopen)
	register(githubops.TypePush, githubops.Push)
	register(githubops.TypePullList, githubops.PullList)
	register(githubops.TypePullView, githubops.PullView)
	register(githubops.TypePullDiff, githubops.PullDiff)
	register(githubops.TypePullCreate, githubops.PullCreate)
	register(githubops.TypePullComment, githubops.PullComment)
	register(githubops.TypePullReview, githubops.PullReview)
	register(githubops.TypePullEdit, githubops.PullEdit)
	register(githubops.TypePullMerge, githubops.PullMerge)
	register(githubops.TypePullClose, githubops.PullClose)
	register(githubops.TypePullReopen, githubops.PullReopen)
	return reg
}

// adapt turns a GitHub operations.Factory into an SDK resourceserver.Factory, binding the
// environment and wrapping the built operation so its authz requirements become the
// SDK's generic requirements.
//
// @arg factory The GitHub operation factory.
// @arg env The execution environment passed to the factory.
// @return resourceserver.Factory An SDK factory building an adapted operation.
//
// @testcase TestRegistryBuildsCloneOperation exercises an adapted factory.
func adapt(factory operations.Factory, env operations.Env) resourceserver.Factory {
	return func(params map[string]any) (resourceserver.Operation, error) {
		op, err := factory(params, env)
		if err != nil {
			return nil, err
		}
		return adapted{op}, nil
	}
}

// adapted wraps a GitHub operations.Operation as a generic resourceserver.Operation, exposing
// its requirements in the SDK's domain-agnostic form.
type adapted struct {
	op operations.Operation
}

// Requirements returns the wrapped operation's requirements converted to the SDK's
// generic Requirement type.
//
// @return []resourceserver.Requirement The converted requirements.
//
// @testcase TestRegistryBuildsCloneOperation checks the converted clone requirement.
func (a adapted) Requirements() []resourceserver.Requirement {
	in := a.op.Requirements()
	out := make([]resourceserver.Requirement, len(in))
	for i, r := range in {
		out[i] = resourceserver.Requirement{Action: r.Action, Resource: convertRef(r.Resource), Context: r.Context}
	}
	return out
}

// Describe forwards to the wrapped operation's description.
//
// @return string The wrapped operation's human-readable summary.
//
// @testcase TestRegistryBuildsCloneOperation reads the adapted description.
func (a adapted) Describe() string { return a.op.Describe() }

// Execute forwards to the wrapped operation's execution.
//
// @arg ctx Context for cancellation.
// @return map[string]any The wrapped operation's structured result.
// @error error from the wrapped operation.
//
// @testcase TestRegistryBuildsCloneOperation executes the adapted clone operation.
func (a adapted) Execute(ctx context.Context) (map[string]any, error) { return a.op.Execute(ctx) }

// convertRef converts an authz.ResourceRef (and its parent chain) into the SDK's
// resourceserver.ResourceRef.
//
// @arg r The GitHub authz resource reference.
// @return resourceserver.ResourceRef The equivalent SDK resource reference.
//
// @testcase TestRegistryBuildsCloneOperation converts a repo resource chain.
func convertRef(r authz.ResourceRef) resourceserver.ResourceRef {
	out := resourceserver.ResourceRef{Type: r.Type, ID: r.ID, Attrs: r.Attrs}
	if r.Parent != nil {
		p := convertRef(*r.Parent)
		out.Parent = &p
	}
	return out
}
