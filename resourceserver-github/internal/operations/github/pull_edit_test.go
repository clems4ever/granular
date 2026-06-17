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

// TestPullEditFactoryValidatesParams checks the pull-edit factory rejects params missing a repo.
func TestPullEditFactoryValidatesParams(t *testing.T) {
	if _, err := PullEdit(map[string]any{"number": 1, "title": "t"}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullEdit(map[string]any{"repo": "o/n", "title": "t"}, operations.Env{}); !errors.Is(err, ErrMissingPullNumber) {
		t.Fatalf("want ErrMissingPullNumber, got %v", err)
	}
	if _, err := PullEdit(map[string]any{"repo": "o/n", "number": 1}, operations.Env{}); !errors.Is(err, ErrNoChanges) {
		t.Fatalf("want ErrNoChanges, got %v", err)
	}
}

// TestPullEditRequirementsAreContentScoped checks different edits produce different content-scoped requirements.
func TestPullEditRequirementsAreContentScoped(t *testing.T) {
	a, _ := PullEdit(map[string]any{"repo": "o/n", "number": 8, "title": "a"}, operations.Env{})
	b, _ := PullEdit(map[string]any{"repo": "o/n", "number": 8, "title": "b"}, operations.Env{})
	ra, rb := a.Requirements()[0], b.Requirements()[0]
	if ra.Action != "pull.edit" || ra.Resource.ID != "o/n#8" {
		t.Fatalf("unexpected requirement %+v", ra)
	}
	if ra.Context["change_hash"] == rb.Context["change_hash"] {
		t.Fatalf("change hash should differ per change set")
	}
}

// TestPullEditDescribe checks the pull-edit description names the repo, pull and changes.
func TestPullEditDescribe(t *testing.T) {
	op, _ := PullEdit(map[string]any{"repo": "o/n", "number": 8, "title": "New title", "base": "develop"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "o/n") || !strings.Contains(d, "#8") || !strings.Contains(d, "New title") || !strings.Contains(d, "develop") {
		t.Fatalf("describe missing repo/number/changes: %q", d)
	}
}

// TestPullEditExecutePatches checks executing a pull edit PATCHes the pull request.
func TestPullEditExecutePatches(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/o/n/pulls/8" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["title"] != "New title" || payload["base"] != "develop" {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"number":8,"title":"New title","html_url":"u"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullEdit(map[string]any{"repo": "o/n", "number": 8, "title": "New title", "base": "develop"}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["title"] != "New title" {
		t.Fatalf("unexpected result: %v", result)
	}
}
