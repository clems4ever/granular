package resourceserver

import (
	"context"
	"fmt"
	"sort"
)

// ResourceRef is an operation-supplied description of a resource being acted on: its
// schema resource type, its identity, optional matcher attributes, and its parent in
// the catalog hierarchy. The SDK turns it into the Cedar entity world the AS evaluates.
type ResourceRef struct {
	Type   string
	ID     string
	Attrs  map[string]any
	Parent *ResourceRef
}

// Requirement is one authorization check an operation needs to pass: an action on a
// resource, optionally qualified by context (e.g. a content hash for writes).
type Requirement struct {
	Action   string
	Resource ResourceRef
	Context  map[string]string
}

// Operation is a single concrete action the resource server can execute on behalf of a client
// once the AS confirms its requirements are authorized. SDK users implement it (or
// adapt an existing implementation) for each action in their schema.
type Operation interface {
	// Requirements returns the authorization checks that must all pass to execute.
	Requirements() []Requirement
	// Describe returns a short human-readable summary of the operation.
	Describe() string
	// Execute performs the operation and returns a structured result.
	Execute(ctx context.Context) (map[string]any, error)
}

// Factory builds an Operation from the request parameters, or reports why the
// parameters are invalid. The factory closes over any platform material it needs (such
// as credentials), keeping the SDK free of domain configuration.
type Factory func(params map[string]any) (Operation, error)

// Registry maps operation type ids to the factories that build them.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry creates an empty operation registry ready to accept Register calls.
//
// @return *Registry A registry with no factories registered yet.
//
// @testcase TestRegistry builds against an empty registry and expects an error.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register associates an operation type id with the factory that builds it, replacing
// any factory previously registered for the same id.
//
// @arg opType The operation type id, e.g. "github.clone".
// @arg factory The factory invoked to build operations of this type.
//
// @testcase TestRegistry registers a factory and builds an operation from it.
func (r *Registry) Register(opType string, factory Factory) {
	r.factories[opType] = factory
}

// Types returns the registered operation type ids in sorted order.
//
// @return []string The registered operation type ids.
//
// @testcase TestRegistry lists the registered types.
func (r *Registry) Types() []string {
	out := make([]string, 0, len(r.factories))
	for t := range r.factories {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Build constructs the Operation described by req using the registered factory for its
// type.
//
// @arg req The wire request naming the operation type and its parameters.
// @return Operation The constructed operation ready to execute.
// @error ErrUnknownType if no factory is registered for the request's type.
//
// @testcase TestRegistry builds a registered operation and errors on an unknown type.
func (r *Registry) Build(req OperationRequest) (Operation, error) {
	factory, ok := r.factories[req.Type]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, req.Type)
	}
	return factory(req.Params)
}

// ErrUnknownType is returned by Build when the request names a type with no registered
// factory.
var ErrUnknownType = fmt.Errorf("unknown operation type")
