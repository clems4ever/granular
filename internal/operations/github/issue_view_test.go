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

func TestIssueViewFactoryValidatesParams(t *testing.T) {
	if _, err := IssueView(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueView(map[string]any{"repo": "owner/name"}, operations.Env{}); !errors.Is(err, ErrMissingIssueNumber) {
		t.Fatalf("want ErrMissingIssueNumber, got %v", err)
	}
}

func TestIssueViewPermissionKeyIncludesNumber(t *testing.T) {
	a, _ := IssueView(map[string]any{"repo": "owner/name", "number": 1}, operations.Env{})
	b, _ := IssueView(map[string]any{"repo": "owner/name", "number": 2}, operations.Env{})
	if a.PermissionKey() == b.PermissionKey() {
		t.Fatalf("number must change the key")
	}
	if a.PermissionKey() != "github.issue.view:owner/name#1" {
		t.Fatalf("unexpected key %q", a.PermissionKey())
	}
}

func TestIssueViewDescribe(t *testing.T) {
	op, _ := IssueView(map[string]any{"repo": "owner/name", "number": 42}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "owner/name") || !strings.Contains(d, "42") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

func TestIssueViewExecuteReturnsRaw(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/name/issues/7" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":7,"title":"the title","state":"open","body":"the body",
			"html_url":"u","comments":3,"user":{"login":"alice"},
			"labels":[{"name":"bug","color":"f00"}],"closed_at":"2020-01-01T00:00:00Z"}`))
	}))
	defer stub.Close()

	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueView(map[string]any{"repo": "owner/name", "number": 7}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Raw pass-through: GitHub's native shape and every attribute preserved.
	if result["title"] != "the title" || result["body"] != "the body" {
		t.Fatalf("unexpected result: %v", result)
	}
	if result["closed_at"] != "2020-01-01T00:00:00Z" {
		t.Fatalf("non-curated attribute closed_at should be preserved: %v", result["closed_at"])
	}
	user, _ := result["user"].(map[string]any)
	if user["login"] != "alice" {
		t.Fatalf("nested user object should be preserved: %v", result["user"])
	}
	labels, _ := result["labels"].([]any)
	if len(labels) != 1 {
		t.Fatalf("labels should remain a raw array: %v", result["labels"])
	}
}
