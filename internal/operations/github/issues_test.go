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

func TestIssueListPermissionKeyIncludesState(t *testing.T) {
	open, _ := IssueList(map[string]any{"repo": "owner/name"}, operations.Env{})
	closed, _ := IssueList(map[string]any{"repo": "owner/name", "state": "closed"}, operations.Env{})
	if open.PermissionKey() == closed.PermissionKey() {
		t.Fatalf("state must change the key")
	}
	if open.PermissionKey() != "github.issue.list:owner/name?state=open" {
		t.Fatalf("unexpected key %q", open.PermissionKey())
	}
}

func TestIssueListDescribe(t *testing.T) {
	op, _ := IssueList(map[string]any{"repo": "owner/name", "state": "closed"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "owner/name") || !strings.Contains(d, "closed") {
		t.Fatalf("describe missing repo/state: %q", d)
	}
}

func TestIssueListExecuteParsesAndFiltersPRs(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/name/issues" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing/incorrect auth header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"number":1,"title":"a real issue","state":"open","html_url":"u1","user":{"login":"alice"}},
			{"number":2,"title":"a pull request","state":"open","html_url":"u2","user":{"login":"bob"},"pull_request":{}}
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
	issues, ok := result["issues"].([]map[string]any)
	if !ok {
		t.Fatalf("issues not a slice: %T", result["issues"])
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue (PR filtered out), got %d", len(issues))
	}
	if issues[0]["title"] != "a real issue" || issues[0]["author"] != "alice" {
		t.Fatalf("unexpected issue: %v", issues[0])
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
