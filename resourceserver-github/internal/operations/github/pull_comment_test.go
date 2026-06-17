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

// TestPullCommentFactoryValidatesParams checks the pull-comment factory rejects params missing a repo.
func TestPullCommentFactoryValidatesParams(t *testing.T) {
	if _, err := PullComment(map[string]any{"number": 1, "body": "b"}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullComment(map[string]any{"repo": "o/n", "body": "b"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
	if _, err := PullComment(map[string]any{"repo": "o/n", "number": 1}, operations.Env{}); !errors.Is(err, ErrMissingBody) {
		t.Fatalf("want ErrMissingBody, got %v", err)
	}
}

// TestPullCommentRequirementsAreContentScoped checks different comment bodies produce different content-scoped requirements.
func TestPullCommentRequirementsAreContentScoped(t *testing.T) {
	a, _ := PullComment(map[string]any{"repo": "o/n", "number": 3, "body": "hello"}, operations.Env{})
	b, _ := PullComment(map[string]any{"repo": "o/n", "number": 3, "body": "world"}, operations.Env{})
	ra, rb := a.Requirements()[0], b.Requirements()[0]
	if ra.Action != "pull.comment" || ra.Resource.ID != "o/n#3" {
		t.Fatalf("unexpected requirement %+v", ra)
	}
	if ra.Context["body_hash"] == rb.Context["body_hash"] {
		t.Fatalf("body hash should differ per body")
	}
}

// TestPullCommentDescribe checks the pull-comment description summarises the target and body.
func TestPullCommentDescribe(t *testing.T) {
	op, _ := PullComment(map[string]any{"repo": "o/n", "number": 3, "body": "the body"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "o/n") || !strings.Contains(d, "#3") || !strings.Contains(d, "the body") {
		t.Fatalf("describe missing repo/number/body: %q", d)
	}
}

// TestPullCommentExecutePosts checks executing a pull comment POSTs to the issue comments endpoint.
func TestPullCommentExecutePosts(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/n/issues/3/comments" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["body"] != "hi" {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"id":99,"html_url":"u"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullComment(map[string]any{"repo": "o/n", "number": 3, "body": "hi"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["html_url"] != "u" {
		t.Fatalf("unexpected result: %v", result)
	}
}
