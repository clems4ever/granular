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

	"github.com/clems4ever/granular/internal/operations"
)

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

func TestIssueClosePermissionKey(t *testing.T) {
	plain, _ := IssueClose(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	if plain.PermissionKey() != "github.issue.close:o/n#5" {
		t.Fatalf("unexpected key %q", plain.PermissionKey())
	}
	withReason, _ := IssueClose(map[string]any{"repo": "o/n", "number": 5, "reason": "not planned"}, operations.Env{})
	if withReason.PermissionKey() != "github.issue.close:o/n#5:not_planned" {
		t.Fatalf("unexpected key %q", withReason.PermissionKey())
	}
}

func TestIssueCloseDescribe(t *testing.T) {
	op, _ := IssueClose(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "#5") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

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

func TestIssueReopenFactoryValidatesParams(t *testing.T) {
	if _, err := IssueReopen(map[string]any{"number": 1}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := IssueReopen(map[string]any{"repo": "o/n"}, operations.Env{}); !errors.Is(err, ErrMissingIssueNumber) {
		t.Fatalf("want ErrMissingIssueNumber, got %v", err)
	}
}

func TestIssueReopenPermissionKey(t *testing.T) {
	op, _ := IssueReopen(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	if op.PermissionKey() != "github.issue.reopen:o/n#5" {
		t.Fatalf("unexpected key %q", op.PermissionKey())
	}
}

func TestIssueReopenDescribe(t *testing.T) {
	op, _ := IssueReopen(map[string]any{"repo": "o/n", "number": 5}, operations.Env{})
	if d := op.Describe(); !strings.Contains(d, "o/n") || !strings.Contains(d, "#5") {
		t.Fatalf("describe missing repo/number: %q", d)
	}
}

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
