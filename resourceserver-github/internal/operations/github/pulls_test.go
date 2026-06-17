package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TestPullListFactoryRequiresRepo checks the pull-list factory rejects params without a repo.
func TestPullListFactoryRequiresRepo(t *testing.T) {
	if _, err := PullList(map[string]any{}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
}

// TestPullListRequirements checks a pull-list operation requires pull.list scoped to the repo and state.
func TestPullListRequirements(t *testing.T) {
	op, _ := PullList(map[string]any{"repo": "owner/name"}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "pull.list" || reqs[0].Resource.ID != "owner/name" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
	if op.Type() != TypePullList {
		t.Fatalf("unexpected type %q", op.Type())
	}
}

// TestPullListDescribe checks the pull-list description names the repo and state.
func TestPullListDescribe(t *testing.T) {
	op, _ := PullList(map[string]any{"repo": "owner/name", "state": "closed"}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "owner/name") || !strings.Contains(d, "closed") {
		t.Fatalf("describe missing repo/state: %q", d)
	}
}

// TestPullListExecuteReturnsRaw checks executing a pull list returns GitHub's raw response.
func TestPullListExecuteReturnsRaw(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/name/pulls" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"number":7,"title":"the pr","state":"open","draft":false,
			"user":{"login":"alice"},"head":{"ref":"feature"},"base":{"ref":"main"}}]`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullList(map[string]any{"repo": "owner/name"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	pulls, ok := result["pulls"].([]any)
	if !ok || len(pulls) != 1 {
		t.Fatalf("expected 1 pull, got %v", result["pulls"])
	}
	first := pulls[0].(map[string]any)
	if first["title"] != "the pr" || first["draft"] != false {
		t.Fatalf("raw attributes should be preserved: %v", first)
	}
}
