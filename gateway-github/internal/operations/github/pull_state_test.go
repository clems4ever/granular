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

	"github.com/clems4ever/granular/gateway-github/internal/operations"
)

// TestPullCloseFactoryValidatesParams checks the pull-close factory rejects params missing a repo.
func TestPullCloseFactoryValidatesParams(t *testing.T) {
	if _, err := PullClose(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullClose(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
}

// TestPullCloseRequirements checks a pull-close operation requires the close action on the pull request.
func TestPullCloseRequirements(t *testing.T) {
	op, _ := PullClose(map[string]any{"repo": "o/n", "number": 9}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "pull.close" || reqs[0].Resource.ID != "o/n#9" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
}

// TestPullCloseDescribe checks the pull-close description names the repo and pull.
func TestPullCloseDescribe(t *testing.T) {
	op, _ := PullClose(map[string]any{"repo": "o/n", "number": 9}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "#9") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

// TestPullCloseExecutePatches checks executing a pull close PATCHes the pull to the closed state.
func TestPullCloseExecutePatches(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if r.Method != http.MethodPatch || payload["state"] != "closed" {
			t.Errorf("unexpected %s payload %v", r.Method, payload)
		}
		_, _ = w.Write([]byte(`{"number":9,"state":"closed"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullClose(map[string]any{"repo": "o/n", "number": 9}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "closed" {
		t.Fatalf("unexpected result: %v", result)
	}
}

// TestPullReopenFactoryValidatesParams checks the pull-reopen factory rejects params missing a repo.
func TestPullReopenFactoryValidatesParams(t *testing.T) {
	if _, err := PullReopen(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullReopen(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
}

// TestPullReopenRequirements checks a pull-reopen operation requires the reopen action on the pull request.
func TestPullReopenRequirements(t *testing.T) {
	op, _ := PullReopen(map[string]any{"repo": "o/n", "number": 9}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "pull.reopen" || reqs[0].Resource.ID != "o/n#9" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
}

// TestPullReopenDescribe checks the pull-reopen description names the repo and pull.
func TestPullReopenDescribe(t *testing.T) {
	op, _ := PullReopen(map[string]any{"repo": "o/n", "number": 9}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "#9") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

// TestPullReopenExecutePatches checks executing a pull reopen PATCHes the pull back to open.
func TestPullReopenExecutePatches(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["state"] != "open" {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"number":9,"state":"open"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullReopen(map[string]any{"repo": "o/n", "number": 9}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "open" {
		t.Fatalf("unexpected result: %v", result)
	}
}
