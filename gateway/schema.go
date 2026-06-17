// Package gateway is the SDK for building a granular gateway (Resource Server). A
// concrete gateway is defined by three domain-specific things the SDK user supplies —
// a Schema (the permission vocabulary: resources, their cataloging hierarchy, and the
// action/verb lattice), a ScopeFunc (how a capability's resource selector maps to a
// Cedar scope), and a set of action implementations (a Registry of Operation
// factories). Everything else — Cedar policy generation, the verify world sent to the
// authorization server (AS), the consent presentation, and the HTTP server — is
// generic and provided here. The SDK never hard-codes any platform vocabulary.
package gateway

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

// Param describes one parameter an operation accepts: its name, value type, whether it
// is required, and what it means. It is the signature a client needs to invoke an
// operation.
type Param struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// OperationSpec describes one executable operation: the type id a client submits to run
// it, the parameters it accepts, whether it mutates, and the action/resource a grant
// must authorize for it to run. It is what an agent reads to actually perform work
// (as opposed to Action, which is the vocabulary a grant request is built from).
type OperationSpec struct {
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Action      string  `json:"action"`
	Resource    string  `json:"resource"`
	Mutating    bool    `json:"mutating"`
	Params      []Param `json:"params"`
	Description string  `json:"description,omitempty"`
}

// ScopeFunc translates a capability's resource selector into the Cedar entity its
// permit's `resource in` clause is scoped to, plus a human-readable label for the
// consent screen. It is the one piece of resource logic that is domain-specific, so
// the SDK user supplies it alongside the Schema.
type ScopeFunc func(sel ResourceSelector) (entityType, entityID, label string, err error)

// Template is a gateway-authored, parameterized permission shape a client can instantiate
// instead of assembling a raw capability. Its parameters bind either to the scope
// selector (Field) or to an attribute condition (Attr+Op), and may be pinned by the
// author (Fixed). The gateway expands a template plus its bindings into a single permit
// and a readable presentation, so the consent screen reads well while the raw policy
// remains inspectable.
type Template struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Summary     string          `json:"summary"` // shown on consent, with {param} placeholders
	Actions     []string        `json:"actions"` // actions or groups granted
	Scope       string          `json:"scope"`   // resource type of the scope selector
	Params      []TemplateParam `json:"params"`
}

// TemplateParam is one parameter of a Template. A param with Field set contributes to the
// scope selector's match; a param with Attr+Op set becomes a Cedar condition on that
// resource attribute. Fixed pins the value (the client cannot bind it); otherwise the
// client supplies it (falling back to Default), and Required rejects an empty result.
type TemplateParam struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	Fixed       string `json:"fixed,omitempty"`
	Field       string `json:"field,omitempty"` // scope: selector match field this fills
	Attr        string `json:"attr,omitempty"`  // condition: resource attribute name
	Op          string `json:"op,omitempty"`    // condition operator: eq | contains | like
}

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

	Resources  []ResourceType  `json:"resources"`
	Groups     []Group         `json:"groups"`
	Actions    []Action        `json:"actions"`
	Operations []OperationSpec `json:"operations"`
	Templates  []Template      `json:"templates"`
	Example    GrantRequest    `json:"request_example"`

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
