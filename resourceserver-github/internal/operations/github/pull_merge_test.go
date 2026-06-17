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

// TestPullMergeFactoryValidatesParams checks the pull-merge factory rejects params missing a repo.
func TestPullMergeFactoryValidatesParams(t *testing.T) {
	if _, err := PullMerge(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullMerge(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
	if _, err := PullMerge(map[string]any{"repo": "o/n", "number": 1, "method": "bogus"}, operations.Env{}); !errors.Is(err, ErrInvalidMergeMethod) {
		t.Fatalf("want ErrInvalidMergeMethod, got %v", err)
	}
	if _, err := PullMerge(map[string]any{"repo": "o/n", "number": 1, "method": "squash"}, operations.Env{}); err != nil {
		t.Fatalf("valid method should pass, got %v", err)
	}
}

// TestPullMergeRequirements checks a pull-merge operation requires the merge action on the pull request.
func TestPullMergeRequirements(t *testing.T) {
	op, _ := PullMerge(map[string]any{"repo": "o/n", "number": 6, "sha": "abc"}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "pull.merge" || reqs[0].Resource.ID != "o/n#6" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
	if reqs[0].Context["method"] != "merge" || reqs[0].Context["sha"] != "abc" {
		t.Fatalf("unexpected context %v", reqs[0].Context)
	}
}

// TestPullMergeDescribe checks the pull-merge description names the pull and merge method.
func TestPullMergeDescribe(t *testing.T) {
	op, _ := PullMerge(map[string]any{"repo": "o/n", "number": 6, "method": "squash"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "o/n") || !strings.Contains(d, "#6") || !strings.Contains(d, "squash") {
		t.Fatalf("describe missing repo/number/method: %q", d)
	}
}

// TestPullMergeExecutePuts checks executing a pull merge PUTs to the merge endpoint.
func TestPullMergeExecutePuts(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/repos/o/n/pulls/6/merge" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["merge_method"] != "squash" {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"merged":true,"sha":"deadbeef"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullMerge(map[string]any{"repo": "o/n", "number": 6, "method": "squash"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["merged"] != true {
		t.Fatalf("unexpected result: %v", result)
	}
}
