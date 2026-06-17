// Package gateway is the SDK for building a granular gateway (Resource Server). A
// concrete gateway is defined by three domain-specific things the SDK user supplies —
// a Schema (the permission vocabulary: resources, their cataloging hierarchy, and the
// action/verb lattice), a ScopeFunc (how a capability's resource selector maps to a
// Cedar scope), and a set of action implementations (a Registry of Operation
// factories). Everything else — Cedar policy generation, the verify world sent to the
// authorization server (AS), the consent presentation, and the HTTP server — is
// generic and provided here. The SDK never hard-codes any platform vocabulary.
package gateway

import "github.com/clems4ever/granular/internal/api"

// Wire-type aliases re-exported so SDK users depend only on the gateway package for
// the request/response shapes a client exchanges with a gateway.
type (
	// Capability grants a set of actions on the resources a selector matches.
	Capability = api.Capability
	// ResourceSelector picks the resources a capability applies to.
	ResourceSelector = api.ResourceSelector
	// GrantRequest is a client's bundle of capabilities to be signed.
	GrantRequest = api.GrantRequest
	// OperationRequest names a concrete operation and its parameters.
	OperationRequest = api.Operation
)

// MatchField is a typed attribute a resource can be matched on in a grant request.
type MatchField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ResourceType is a node in the typed resource hierarchy. Entity is its Cedar
// entity-type name (binding the schema to the policy engine) and Parent names the
// resource it is cataloged under, forming the hierarchy used for scope roll-ups.
type ResourceType struct {
	Name        string       `json:"name"`
	Title       string       `json:"title"`
	Entity      string       `json:"entity"`
	Parent      string       `json:"parent,omitempty"`
	Description string       `json:"description"`
	Match       []MatchField `json:"match"`
}

// Group is a verb-lattice node: a roll-up that nests other groups (via Parents) and
// ultimately the concrete actions.
type Group struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Parents     []string `json:"parents,omitempty"`
}

// Action is a concrete action in the vocabulary: what resource it acts on, which
// groups it rolls up into, and human-readable text shown on the consent screen.
type Action struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Resource    string   `json:"resource"`
	Groups      []string `json:"groups"`
	Description string   `json:"description"`
}

// ScopeFunc translates a capability's resource selector into the Cedar entity its
// permit's `resource in` clause is scoped to, plus a human-readable label for the
// consent screen. It is the one piece of resource logic that is domain-specific, so
// the SDK user supplies it alongside the Schema.
type ScopeFunc func(sel ResourceSelector) (entityType, entityID, label string, err error)

// Schema is the permission vocabulary a gateway exposes. Resources, Groups and Actions
// are served to clients (as JSON) so they can build grant requests; AgentType,
// ActionType, AgentID and Scope drive Cedar policy and verify-world generation and are
// not serialized.
type Schema struct {
	// AgentType, ActionType and AgentID are the Cedar entity types and id used for the
	// principal and actions, e.g. "GitHub::Agent", "GitHub::Action" and "agent".
	AgentType  string `json:"-"`
	ActionType string `json:"-"`
	AgentID    string `json:"-"`

	Resources []ResourceType   `json:"resources"`
	Groups    []Group          `json:"groups"`
	Actions   []Action         `json:"actions"`
	Example   api.GrantRequest `json:"request_example"`

	// Scope maps a capability selector to a Cedar scope entity; supplied by the user.
	Scope ScopeFunc `json:"-"`
}

// HasAction reports whether name is a known concrete action or group in the schema.
//
// @arg name The action or group name to check.
// @return bool True when name appears in the action lattice.
//
// @testcase TestSchemaHelpers resolves known and unknown action names.
func (s Schema) HasAction(name string) bool {
	_, ok := s.ActionLattice()[name]
	return ok
}

// ResourceEntity returns the Cedar entity type for a schema resource name.
//
// @arg name The resource name, e.g. "github.repo".
// @return string The Cedar entity type, e.g. "GitHub::Repo".
// @return bool True when the resource name is known.
//
// @testcase TestSchemaHelpers resolves a known resource type and rejects an unknown one.
func (s Schema) ResourceEntity(name string) (string, bool) {
	for _, r := range s.Resources {
		if r.Name == name {
			return r.Entity, true
		}
	}
	return "", false
}

// ActionLattice returns the verb lattice as a flat map of every action and group name
// to its direct parent group names. It is the single source the Cedar layer uses to
// build its action-group entities.
//
// @return map[string][]string Each action/group name mapped to its parent groups.
//
// @testcase TestSchemaHelpers checks groups and actions appear in the lattice.
func (s Schema) ActionLattice() map[string][]string {
	lattice := make(map[string][]string, len(s.Groups)+len(s.Actions))
	for _, g := range s.Groups {
		lattice[g.Name] = g.Parents
	}
	for _, a := range s.Actions {
		lattice[a.Name] = a.Groups
	}
	return lattice
}
