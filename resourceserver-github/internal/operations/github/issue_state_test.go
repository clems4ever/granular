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

// TestIssueCloseFactoryValidatesParams checks the issue-close factory rejects params missing a repo.
func TestIssueCloseFactoryValidatesParams(t *testing.T) {
	if _, err := IssueClose(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueClose(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingIssueNumber) {
		t.Fatalf("want ErrMissingIssueNumber, got %v", err)
	}
	if _, err := IssueClose(map[string]any{"repo": "o/n", "number": 1, "reason": "bogus"}, operations.Env{}); !errors.Is(err, ErrInvalidCloseReason) {
		t.Fatalf("want ErrInvalidCloseReason, got %v", err)
	}
	if _, err := IssueClose(map[string]any{"repo": "o/n", "number": 1, "reason": "not planned"}, operations.Env{}); err != nil {
		t.Fatalf("valid reason should pass, got %v", err)
	}
}

// TestIssueCloseRequirements checks an issue-close operation requires the close action on the issue.
func TestIssueCloseRequirements(t *testing.T) {
	op, _ := IssueClose(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "issue.close" || reqs[0].Resource.ID != "o/n#5" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
}

// TestIssueCloseDescribe checks the issue-close description names the repo and issue.
func TestIssueCloseDescribe(t *testing.T) {
	op, _ := IssueClose(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "#5") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

// TestIssueCloseExecutePatches checks executing an issue close PATCHes the issue to the closed state.
func TestIssueCloseExecutePatches(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/o/n/issues/5" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["state"] != "closed" || payload["state_reason"] != "not_planned" {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"number":5,"state":"closed","html_url":"u"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueClose(map[string]any{"repo": "o/n", "number": 5, "reason": "not planned"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "closed" {
		t.Fatalf("unexpected result: %v", result)
	}
}

// TestIssueReopenFactoryValidatesParams checks the issue-reopen factory rejects params missing a repo.
func TestIssueReopenFactoryValidatesParams(t *testing.T) {
	if _, err := IssueReopen(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueReopen(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingIssueNumber) {
		t.Fatalf("want ErrMissingIssueNumber, got %v", err)
	}
}

// TestIssueReopenRequirements checks an issue-reopen operation requires the reopen action on the issue.
func TestIssueReopenRequirements(t *testing.T) {
	op, _ := IssueReopen(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	reqs := op.Requirements()
	if len(reqs) != 1 || reqs[0].Action != "issue.reopen" || reqs[0].Resource.ID != "o/n#5" {
		t.Fatalf("unexpected requirements %+v", reqs)
	}
}

// TestIssueReopenDescribe checks the issue-reopen description names the repo and issue.
func TestIssueReopenDescribe(t *testing.T) {
	op, _ := IssueReopen(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "#5") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

// TestIssueReopenExecutePatches checks executing an issue reopen PATCHes the issue back to open.
func TestIssueReopenExecutePatches(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["state"] != "open" {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"number":5,"state":"open"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := IssueReopen(map[string]any{"repo": "o/n", "number": 5}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["state"] != "open" {
		t.Fatalf("unexpected result: %v", result)
	}
}
