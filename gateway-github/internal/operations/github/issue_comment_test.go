package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/gateway-github/internal/operations"
)

// TestIssueCommentFactoryValidatesParams checks the issue-comment factory rejects params missing a repo.
func TestIssueCommentFactoryValidatesParams(t *testing.T) {
	if _, err := IssueComment(map[string]any{"number": 1, "body": "x"}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueComment(map[string]any{"repo": "o/n", "body": "x"}, operations.Env{}); !errors.Is(err, ErrMissingIssueNumber) {
		t.Fatalf("want ErrMissingIssueNumber, got %v", err)
	}
	if _, err := IssueComment(map[string]any{"repo": "o/n", "number": 1}, operations.Env{}); !errors.Is(err, ErrMissingBody) {
		t.Fatalf("want ErrMissingBody, got %v", err)
	}
}

// TestIssueCommentRequirementsAreContentScoped checks different comment bodies produce different content-scoped requirements.
func TestIssueCommentRequirementsAreContentScoped(t *testing.T) {
	a, _ := IssueComment(map[string]any{"repo": "o/n", "number": 1, "body": "hello"}, operations.Env{})
	b, _ := IssueComment(map[string]any{"repo": "o/n", "number": 1, "body": "different"}, operations.Env{})
	ra, rb := a.Requirements(), b.Requirements()
	if ra[0].Action != "issue.comment" || ra[0].Resource.ID != "o/n#1" {
		t.Fatalf("unexpected requirement %+v", ra[0])
	}
	if ra[0].Context["body_hash"] == "" || ra[0].Context["body_hash"] == rb[0].Context["body_hash"] {
		t.Fatalf("different bodies must yield different body_hash context")
	}
}

// TestIssueCommentDescribe checks the issue-comment description summarises the target and body.
func TestIssueCommentDescribe(t *testing.T) {
	op, _ := IssueComment(map[string]any{"repo": "o/n", "number": 5, "body": "the comment text"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "o/n") || !strings.Contains(d, "#5") || !strings.Contains(d, "the comment text") {
		t.Fatalf("describe missing repo/number/body: %q", d)
	}
}

// TestIssueCommentExecutePosts checks executing an issue comment POSTs to the issue's comments endpoint.
func TestIssueCommentExecutePosts(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/n/issues/5/comments" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":99,"html_url":"https://github.com/o/n/issues/5#issuecomment-99","body":"hi"}`))
	}))
	defer stub.Close()

	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueComment(map[string]any{"repo": "o/n", "number": 5, "body": "hi"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["html_url"] != "https://github.com/o/n/issues/5#issuecomment-99" {
		t.Fatalf("unexpected result: %v", result)
	}
}
