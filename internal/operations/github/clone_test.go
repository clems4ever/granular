package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/operations"
)

func TestCloneFactoryRequiresRepo(t *testing.T) {
	if _, err := Clone(map[string]any{}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
}

func TestCloneRequirements(t *testing.T) {
	op, err := Clone(map[string]any{"repo": "https://github.com/owner/name.git"}, operations.Env{})
	if err != nil {
		t.Fatal(err)
	}
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "repo.clone" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
	if reqs[0].Resource.Type != "github.repo" || reqs[0].Resource.ID != "owner/name" {
		t.Fatalf("unexpected resource %+v", reqs[0].Resource)
	}
}

func TestCloneDescribe(t *testing.T) {
	op, _ := Clone(map[string]any{"repo": "owner/name"}, operations.Env{})
	if !strings.Contains(op.Describe(), "owner/name") {
		t.Fatalf("describe missing repo: %q", op.Describe())
	}
}

func TestExecuteReturnsProxyCloneURL(t *testing.T) {
	op, _ := Clone(map[string]any{"repo": "owner/name"}, operations.Env{BaseURL: "http://localhost:8080"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := result["clone_url"]; got != "http://localhost:8080/git/owner/name.git" {
		t.Fatalf("unexpected clone_url %v", got)
	}
}

func TestNormalizeRepo(t *testing.T) {
	cases := map[string]string{
		"owner/name":                        "owner/name",
		"github.com/owner/name":             "owner/name",
		"https://github.com/owner/name.git": "owner/name",
		"git@github.com:owner/name.git":     "owner/name",
	}
	for in, want := range cases {
		if got := NormalizeRepo(in); got != want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
