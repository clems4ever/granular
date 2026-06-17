package operations

import (
	"context"
	"errors"
	"testing"

	"github.com/clems4ever/granular/gateway-github/internal/authz"
	"github.com/clems4ever/granular/internal/api"
)

// stubOp is a minimal Operation used to exercise the registry.
type stubOp struct{}

// Type returns the stub operation type id.
func (stubOp) Type() string { return "stub" }

// Requirements returns no authorization requirements.
func (stubOp) Requirements() []authz.Requirement { return nil }

// Describe returns the stub operation summary.
func (stubOp) Describe() string { return "stub" }

// Execute returns an empty success result.
func (stubOp) Execute(ctx context.Context) (map[string]any, error) { return map[string]any{}, nil }

// TestRegistryBuildUnknownType checks Build returns ErrUnknownType for an unregistered type.
func TestRegistryBuildUnknownType(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Build(api.Operation{Type: "nope"}, Env{}); !errors.Is(err, ErrUnknownType) {
		t.Fatalf("want ErrUnknownType, got %v", err)
	}
}

// TestRegistryBuildKnownType checks Build invokes the registered factory and returns its operation.
func TestRegistryBuildKnownType(t *testing.T) {
	reg := NewRegistry()
	called := false
	reg.Register("stub", func(params map[string]any, env Env) (Operation, error) {
		called = true
		return stubOp{}, nil
	})
	op, err := reg.Build(api.Operation{Type: "stub", Params: map[string]any{"x": 1}}, Env{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !called {
		t.Fatal("factory was not invoked")
	}
	if op.Type() != "stub" {
		t.Fatalf("unexpected operation type %q", op.Type())
	}
}
