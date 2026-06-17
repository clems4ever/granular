// Package operations defines the Operation abstraction — a concrete,
// parameterised action the resource server executes on a third-party platform once the
// authorization server confirms a human has approved it. The resource server SDK's
// registry builds and dispatches these operations.
package operations

import (
	"context"

	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
)

// Env carries the server-held material an operation needs, such as platform
// credentials. It is passed to each factory so operations never reach for global
// state.
type Env struct {
	// GitHubToken is the personal access token the server injects when proxying
	// GitHub requests on the client's behalf.
	GitHubToken string
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
