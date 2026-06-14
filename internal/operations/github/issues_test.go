package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/operations"
)

func TestIssueListFactoryRequiresRepo(t *testing.T) {
	if _, err := IssueList(map[string]any{}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
}

func TestIssueListRequirements(t *testing.T) {
	op, _ := IssueList(map[string]any{"repo": "owner/name"}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "issue.list" || reqs[0].Resource.Type != "github.repo" || reqs[0].Resource.ID != "owner/name" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
}

func TestIssueListDescribe(t *testing.T) {
	op, _ := IssueList(map[string]any{"repo": "owner/name", "state": "closed"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "owner/name") || !strings.Contains(d, "closed") {
		t.Fatalf("describe missing repo/state: %q", d)
	}
}

func TestIssueListExecuteReturnsRaw(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/name/issues" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing/incorrect auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"a real issue","state":"open","html_url":"u1","user":{"login":"alice"},"locked":false},
			{"number":2,"title":"a pull request","state":"open","html_url":"u2","user":{"login":"bob"},"pull_request":{"url":"p"}}
		]`))
	}))
	defer stub.Close()

	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueList(map[string]any{"repo": "owner/name"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	issues, ok := result["issues"].([]any)
	if !ok {
		t.Fatalf("issues not a slice: %T", result["issues"])
	}
	// Raw pass-through: nothing filtered, all attributes preserved.
	if len(issues) != 2 {
		t.Fatalf("expected 2 raw items (no filtering), got %d", len(issues))
	}
	first := issues[0].(map[string]any)
	if first["title"] != "a real issue" || first["locked"] != false {
		t.Fatalf("raw attributes not preserved: %v", first)
	}
	if _, hasPR := issues[1].(map[string]any)["pull_request"]; !hasPR {
		t.Fatalf("pull_request attribute should be preserved in raw output")
	}
}

func TestIntParam(t *testing.T) {
	if got := intParam(map[string]any{"limit": float64(5)}, "limit", 30); got != 5 {
		t.Errorf("float64: got %d", got)
	}
	if got := intParam(map[string]any{"limit": "7"}, "limit", 30); got != 7 {
		t.Errorf("string: got %d", got)
	}
	if got := intParam(map[string]any{}, "limit", 30); got != 30 {
		t.Errorf("fallback: got %d", got)
	}
}
