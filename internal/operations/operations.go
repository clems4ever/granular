// Package operations defines the Operation abstraction — a concrete,
// parameterised action that the server executes on a third-party platform once a
// human has approved it — together with a registry that builds operations from
// wire requests.
package operations

import (
	"context"
	"fmt"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/authz"
)

// Env carries the server-held material an operation needs, such as platform
// credentials and the server's public base URL (used to build brokered endpoints
// like the git proxy). It is passed to each factory so operations never reach for
// global state.
type Env struct {
	// GitHubToken is the personal access token the server injects when proxying
	// GitHub requests on the client's behalf.
	GitHubToken string
	// BaseURL is the server's externally reachable base URL, used to build
	// brokered URLs (e.g. the git proxy clone URL) handed back to the client.
	BaseURL string
}

// Operation is a single approved-action unit: it can describe itself, derive the
// permission key a grant is matched against, and execute server-side.
type Operation interface {
	// Type returns the operation's type id, e.g. "github.clone".
	Type() string
	// Requirements returns the authorization checks (action on resource, optionally
	// context-qualified) that must all pass for the operation to be allowed.
	Requirements() []authz.Requirement
	// Describe returns a short human-readable summary shown on the approval page.
	Describe() string
	// Execute performs the operation and returns a structured result.
	Execute(ctx context.Context) (map[string]any, error)
}

// Factory builds an Operation from the request parameters and the server Env, or
// reports why the parameters are invalid.
type Factory func(params map[string]any, env Env) (Operation, error)

// Registry maps operation type ids to the factories that build them.
type Registry struct {
	factories map[string]Factory
}

// NewRegistry creates an empty operation registry ready to accept Register calls.
//
// @return *Registry A registry with no factories registered yet.
//
// @testcase TestRegistryBuildUnknownType builds against an empty registry and expects an error.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register associates an operation type id with the factory that builds it,
// overwriting any factory previously registered for the same id.
//
// @arg opType The operation type id, e.g. "github.clone".
// @arg factory The factory invoked to build operations of this type.
//
// @testcase TestRegistryBuildKnownType registers a factory and builds an operation from it.
func (r *Registry) Register(opType string, factory Factory) {
	r.factories[opType] = factory
}

// Build constructs the Operation described by req using the registered factory for
// req.Type and the supplied Env.
//
// @arg req The wire request naming the operation type and its parameters.
// @arg env The server material (credentials, workspace) handed to the factory.
// @return Operation The constructed operation ready to execute.
// @error ErrUnknownType if no factory is registered for req.Type.
//
// @testcase TestRegistryBuildKnownType builds a registered operation successfully.
// @testcase TestRegistryBuildUnknownType returns an error for an unregistered type.
func (r *Registry) Build(req api.OperationRequest, env Env) (Operation, error) {
	factory, ok := r.factories[req.Type]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownType, req.Type)
	}
	return factory(req.Params, env)
}

// ErrUnknownType is returned by Build when the request names a type with no
// registered factory.
var ErrUnknownType = fmt.Errorf("unknown operation type")
