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

// TestPullReviewFactoryValidatesParams checks the pull-review factory rejects params missing a repo.
func TestPullReviewFactoryValidatesParams(t *testing.T) {
	if _, err := PullReview(map[string]any{"number": 1, "event": "approve"}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullReview(map[string]any{"repo": "o/n", "event": "approve"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
	if _, err := PullReview(map[string]any{"repo": "o/n", "number": 1, "event": "bogus"}, operations.Env{}); !errors.Is(err, ErrInvalidReviewEvent) {
		t.Fatalf("want ErrInvalidReviewEvent, got %v", err)
	}
	if _, err := PullReview(map[string]any{"repo": "o/n", "number": 1, "event": "comment"}, operations.Env{}); !errors.Is(err, ErrMissingBody) {
		t.Fatalf("comment review without body should fail, got %v", err)
	}
	if _, err := PullReview(map[string]any{"repo": "o/n", "number": 1, "event": "approve"}, operations.Env{}); err != nil {
		t.Fatalf("approve without body should pass, got %v", err)
	}
}

// TestPullReviewRequirementsAreContentScoped checks different review events or bodies produce different content-scoped requirements.
func TestPullReviewRequirementsAreContentScoped(t *testing.T) {
	a, _ := PullReview(map[string]any{"repo": "o/n", "number": 4, "event": "approve"}, operations.Env{})
	b, _ := PullReview(map[string]any{"repo": "o/n", "number": 4, "event": "request_changes", "body": "no"}, operations.Env{})
	ra, rb := a.Requirements()[0], b.Requirements()[0]
	if ra.Action != "pull.review" || ra.Resource.ID != "o/n#4" {
		t.Fatalf("unexpected requirement %+v", ra)
	}
	if ra.Context["review_hash"] == rb.Context["review_hash"] {
		t.Fatalf("review hash should differ per verdict/body")
	}
}

// TestPullReviewDescribe checks the pull-review description names the pull and event.
func TestPullReviewDescribe(t *testing.T) {
	op, _ := PullReview(map[string]any{"repo": "o/n", "number": 4, "event": "request_changes", "body": "please fix"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "o/n") || !strings.Contains(d, "#4") || !strings.Contains(d, "request changes") {
		t.Fatalf("describe missing repo/number/verdict: %q", d)
	}
}

// TestPullReviewExecutePosts checks executing a pull review POSTs to the reviews endpoint.
func TestPullReviewExecutePosts(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/n/pulls/4/reviews" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["event"] != "APPROVE" {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"id":55,"state":"APPROVED"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullReview(map[string]any{"repo": "o/n", "number": 4, "event": "approve"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "APPROVED" {
		t.Fatalf("unexpected result: %v", result)
	}
}
