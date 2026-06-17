package resourceserver

// This file defines the request wire-shapes a client exchanges with a resource server. The
// resource server SDK owns them: a client builds a GrantRequest (freeform Capabilities or a
// Template instantiation) for signing, or names a concrete OperationRequest to run.

// OperationRequest names a concrete operation and the free-form parameters that
// configure it. A client submits it to run an operation, which the resource server executes
// once the authorization server confirms it is allowed.
type OperationRequest struct {
	Type   string         `json:"type"`
	Params map[string]any `json:"params,omitempty"`
}

// GrantRequest is a client's request to be granted access for later use. It names the
// actions and resources to pre-approve; unlike an OperationRequest it never executes
// anything, it only asks a human to authorise the scope. It is built either freeform
// (Capabilities) or by instantiating a resource server Template (Template + Bindings); a
// resource server rejects a request that sets both or neither.
type GrantRequest struct {
	Reason       string       `json:"reason,omitempty"`
	Capabilities []Capability `json:"capabilities,omitempty"`

	// Template names a resource server-defined template to instantiate; Bindings supplies its
	// parameter values. When Template is set, Capabilities must be empty.
	Template string            `json:"template,omitempty"`
	Bindings map[string]string `json:"bindings,omitempty"`
}

// Capability grants a set of actions (catalog action or group names) on the resources
// a selector matches.
type Capability struct {
	Actions  []string         `json:"actions"`
	Resource ResourceSelector `json:"resource"`
}

// ResourceSelector picks the resources a capability applies to: a catalog resource
// type plus matcher fields (e.g. {"owner":"clems4ever","name":"granular"}; a "*" value
// widens, e.g. name "*" means all repos under the owner).
type ResourceSelector struct {
	Type  string            `json:"type"`
	Match map[string]string `json:"match"`
}
