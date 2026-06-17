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

// TestPullViewFactoryValidatesParams checks the pull-view factory rejects params missing a repo.
func TestPullViewFactoryValidatesParams(t *testing.T) {
	if _, err := PullView(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullView(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
}

// TestPullViewRequirements checks a pull-view operation requires pull.view on the specific pull request.
func TestPullViewRequirements(t *testing.T) {
	op, _ := PullView(map[string]any{"repo": "o/n", "number": 7}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "pull.view" || reqs[0].Resource.ID != "o/n#7" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
}

// TestPullViewCommentsAddsRequirement checks the --comments view adds a separate comment.read requirement.
func TestPullViewCommentsAddsRequirement(t *testing.T) {
	plain, _ := PullView(map[string]any{"repo": "o/n", "number": 7}, operations.Env{})
	withC, _ := PullView(map[string]any{"repo": "o/n", "number": 7, "comments": true}, operations.Env{})
	if len(plain.Requirements()) != 1 {
		t.Fatalf("plain view should have one requirement, got %+v", plain.Requirements())
	}
	reqs := withC.Requirements()
	if len(reqs) != 2 || reqs[1].Action != "comment.read" {
		t.Fatalf("--comments should add a comment.read requirement, got %+v", reqs)
	}
}

// TestPullViewDescribe checks the pull-view description names the repo and pull number.
func TestPullViewDescribe(t *testing.T) {
	op, _ := PullView(map[string]any{"repo": "o/n", "number": 42}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "42") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

// TestPullViewExecuteReturnsRaw checks executing a pull view returns GitHub's raw pull object.
func TestPullViewExecuteReturnsRaw(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/n/pulls/7" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":7,"title":"the pr","state":"open","body":"the body",
			"merged":false,"mergeable_state":"clean","head":{"ref":"feature"},"base":{"ref":"main"}}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullView(map[string]any{"repo": "o/n", "number": 7}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["title"] != "the pr" || result["body"] != "the body" {
		t.Fatalf("unexpected result: %v", result)
	}
	if result["mergeable_state"] != "clean" {
		t.Fatalf("non-curated attribute mergeable_state should be preserved: %v", result["mergeable_state"])
	}
}

// TestPullViewExecuteWithComments checks executing a view with comments folds the comments into the result.
func TestPullViewExecuteWithComments(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/n/pulls/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":7,"title":"t"}`))
	})
	mux.HandleFunc("/repos/o/n/issues/7/comments", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"body":"convo","user":{"login":"alice"}}]`))
	})
	mux.HandleFunc("/repos/o/n/pulls/7/comments", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"body":"inline","path":"a.go"}]`))
	})
	mux.HandleFunc("/repos/o/n/pulls/7/reviews", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"state":"APPROVED","user":{"login":"bob"}}]`))
	})
	stub := httptest.NewServer(mux)
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullView(map[string]any{"repo": "o/n", "number": 7, "comments": true}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	comments, _ := result["comments_list"].([]any)
	reviewComments, _ := result["review_comments_list"].([]any)
	reviews, _ := result["reviews_list"].([]any)
	if len(comments) != 1 || len(reviewComments) != 1 || len(reviews) != 1 {
		t.Fatalf("expected each conversation array populated: %v", result)
	}
}

// TestPullDiffFactoryValidatesParams checks the pull-diff factory rejects params missing a repo.
func TestPullDiffFactoryValidatesParams(t *testing.T) {
	if _, err := PullDiff(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullDiff(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
}

// TestPullDiffRequirements checks a pull-diff operation requires pull.view on the pull request.
func TestPullDiffRequirements(t *testing.T) {
	op, _ := PullDiff(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "pull.diff" || reqs[0].Resource.ID != "o/n#5" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
	if op.Type() != TypePullDiff {
		t.Fatalf("unexpected type %q", op.Type())
	}
}

// TestPullDiffDescribe checks the pull-diff description names the repo and pull number.
func TestPullDiffDescribe(t *testing.T) {
	op, _ := PullDiff(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "5") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

// TestPullDiffExecuteReturnsRaw checks executing a pull diff returns the raw unified diff.
func TestPullDiffExecuteReturnsRaw(t *testing.T) {
	const diff = "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n"
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accept := r.Header.Get("Accept"); accept != "application/vnd.github.diff" {
			t.Errorf("unexpected Accept %q", accept)
		}
		_, _ = w.Write([]byte(diff))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullDiff(map[string]any{"repo": "o/n", "number": 5}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["diff"] != diff {
		t.Fatalf("diff should be passed through verbatim, got %q", result["diff"])
	}
}
