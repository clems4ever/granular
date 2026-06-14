package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/operations"
)

func TestPushFactoryRequiresRepo(t *testing.T) {
	if _, err := Push(map[string]any{}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
}

func TestPushRequirements(t *testing.T) {
	op, _ := Push(map[string]any{"repo": "owner/name"}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "repo.push" || reqs[0].Resource.ID != "owner/name" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
	if op.Type() != TypePush {
		t.Fatalf("unexpected type %q", op.Type())
	}
}

func TestPushDescribe(t *testing.T) {
	op, _ := Push(map[string]any{"repo": "owner/name"}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "owner/name") {
		t.Fatalf("describe missing repo: %q", d)
	}
}

func TestPushExecuteReturnsProxyURL(t *testing.T) {
	op, _ := Push(map[string]any{"repo": "owner/name"}, operations.Env{BaseURL: "http://localhost:8080/"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["push_url"] != "http://localhost:8080/git/owner/name.git" {
		t.Fatalf("unexpected push_url: %v", result["push_url"])
	}
	if result["repo"] != "owner/name" {
		t.Fatalf("unexpected repo: %v", result["repo"])
	}
}
