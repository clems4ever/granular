// Package verify holds the generic, domain-agnostic wire types for the gateway→AS
// authorization check (POST /api/verify). The gateway supplies the whole Cedar world
// (entities + action lattice) and the questions; the AS evaluates them against the
// opaque policies attached to a token, never interpreting their meaning.
package verify

// EntityRef identifies a Cedar entity by type and id.
type EntityRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Entity is one Cedar entity: its uid, hierarchy parents and attributes.
type Entity struct {
	Type    string         `json:"type"`
	ID      string         `json:"id"`
	Parents []EntityRef    `json:"parents,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// Request is one authorization question: an action by a principal on a resource,
// optionally qualified by context.
type Request struct {
	Principal EntityRef         `json:"principal"`
	Action    EntityRef         `json:"action"`
	Resource  EntityRef         `json:"resource"`
	Context   map[string]string `json:"context,omitempty"`
}

// Input is the body a gateway posts to POST /api/verify: the policy token plus the
// questions and the entity world to evaluate them against.
type Input struct {
	Token    string    `json:"token"`
	Requests []Request `json:"requests"`
	Entities []Entity  `json:"entities"`
}

// Output is the AS's decision.
type Output struct {
	Allowed bool   `json:"allowed"`
	Error   string `json:"error,omitempty"`
}
