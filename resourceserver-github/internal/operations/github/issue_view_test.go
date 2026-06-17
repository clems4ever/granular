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

// TestIssueViewFactoryValidatesParams checks the issue-view factory rejects params missing a repo.
func TestIssueViewFactoryValidatesParams(t *testing.T) {
	if _, err := IssueView(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueView(map[string]any{"repo": "owner/name"}, operations.Env{}); !errors.Is(err, ErrMissingIssueNumber) {
		t.Fatalf("want ErrMissingIssueNumber, got %v", err)
	}
}

// TestIssueViewRequirements checks an issue-view operation requires issue.view on the specific issue.
func TestIssueViewRequirements(t *testing.T) {
	op, _ := IssueView(map[string]any{"repo": "owner/name", "number": 1}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "issue.view" || reqs[0].Resource.ID != "owner/name#1" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
}

// TestIssueViewDescribe checks the issue-view description names the repo and issue number.
func TestIssueViewDescribe(t *testing.T) {
	op, _ := IssueView(map[string]any{"repo": "owner/name", "number": 42}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "owner/name") || !strings.Contains(d, "42") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

// TestIssueViewCommentsAddsRequirement checks the --comments view adds a separate comment.read requirement.
func TestIssueViewCommentsAddsRequirement(t *testing.T) {
	plain, _ := IssueView(map[string]any{"repo": "owner/name", "number": 7}, operations.Env{})
	withC, _ := IssueView(map[string]any{"repo": "owner/name", "number": 7, "comments": true}, operations.Env{})
	if len(plain.Requirements()) != 1 {
		t.Fatalf("plain view should have one requirement, got %+v", plain.Requirements())
	}
	reqs := withC.Requirements()
	if len(reqs) != 2 || reqs[1].Action != "comment.read" {
		t.Fatalf("--comments should add a comment.read requirement, got %+v", reqs)
	}
}

// TestIssueViewExecuteWithComments checks executing a view with comments folds the comments array into the result.
func TestIssueViewExecuteWithComments(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/name/issues/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":7,"title":"t","comments":2}`))
	})
	mux.HandleFunc("/repos/owner/name/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"body":"first","user":{"login":"alice"}},{"body":"second","user":{"login":"bob"}}]`))
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()

	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueView(map[string]any{"repo": "owner/name", "number": 7, "comments": true}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	comments, ok := result["comments_list"].([]any)
	if !ok || len(comments) != 2 {
		t.Fatalf("expected 2 comments under comments_list, got %v", result["comments_list"])
	}
	if comments[0].(map[string]any)["body"] != "first" {
		t.Fatalf("unexpected comment body: %v", comments[0])
	}
}

// TestIssueViewExecuteReturnsRaw checks executing an issue view returns GitHub's raw issue object.
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
