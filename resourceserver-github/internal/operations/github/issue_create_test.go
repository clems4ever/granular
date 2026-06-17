package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/resourceserver-github/internal/operations"
)

// TestIssueCreateFactoryValidatesParams checks the issue-create factory rejects params missing a repo.
func TestIssueCreateFactoryValidatesParams(t *testing.T) {
	if _, err := IssueCreate(map[string]any{"title": "t"}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueCreate(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingTitle) {
		t.Fatalf("want ErrMissingTitle, got %v", err)
	}
}

// TestIssueCreateRequirementsAreContentScoped checks different issue contents produce different content-scoped requirements.
func TestIssueCreateRequirementsAreContentScoped(t *testing.T) {
	a, _ := IssueCreate(map[string]any{"repo": "o/n", "title": "Bug", "labels": []any{"p1"}}, operations.Env{})
	b, _ := IssueCreate(map[string]any{"repo": "o/n", "title": "Bug", "labels": []any{"p2"}}, operations.Env{})
	ra, rb := a.Requirements(), b.Requirements()
	if ra[0].Action != "issue.create" || ra[0].Resource.Type != "github.repo" {
		t.Fatalf("unexpected requirement %+v", ra[0])
	}
	if ra[0].Context["content_hash"] == rb[0].Context["content_hash"] {
		t.Fatalf("different labels must yield different content_hash context")
	}
}

// TestIssueCreateDescribe checks the issue-create description names the repo and title.
func TestIssueCreateDescribe(t *testing.T) {
	op, _ := IssueCreate(map[string]any{"repo": "o/n", "title": "My bug"}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "My bug") {
		t.Fatalf("describe missing repo/title: %q", d)
	}
}

// TestIssueCreateExecutePosts checks executing an issue create POSTs to the repo's issues endpoint.
func TestIssueCreateExecutePosts(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/n/issues" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["title"] != "My bug" {
			t.Errorf("unexpected payload title: %v", payload["title"])
		}
		if _, hasLabels := payload["labels"]; !hasLabels {
			t.Errorf("labels should be included in payload")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42,"html_url":"https://github.com/o/n/issues/42","title":"My bug"}`))
	}))
	defer stub.Close()

	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueCreate(map[string]any{"repo": "o/n", "title": "My bug", "body": "details", "labels": []any{"bug"}}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["number"] != float64(42) {
		t.Fatalf("unexpected result: %v", result)
	}
}
