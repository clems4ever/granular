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

// TestPullCreateFactoryValidatesParams checks the pull-create factory rejects params missing a repo.
func TestPullCreateFactoryValidatesParams(t *testing.T) {
	if _, err := PullCreate(map[string]any{"title": "t", "head": "f", "base": "m"}, operations.Env{}); !errors.Is(err, ErrMissingRepo) {
		t.Fatalf("want ErrMissingRepo, got %v", err)
	}
	if _, err := PullCreate(map[string]any{"repo": "o/n", "head": "f", "base": "m"}, operations.Env{}); !errors.Is(err, ErrMissingTitle) {
		t.Fatalf("want ErrMissingTitle, got %v", err)
	}
	if _, err := PullCreate(map[string]any{"repo": "o/n", "title": "t", "head": "f"}, operations.Env{}); !errors.Is(err, ErrMissingBranches) {
		t.Fatalf("want ErrMissingBranches, got %v", err)
	}
}

// TestPullCreateRequirementsAreContentScoped checks different pull contents produce different content-scoped requirements.
func TestPullCreateRequirementsAreContentScoped(t *testing.T) {
	a, _ := PullCreate(map[string]any{"repo": "o/n", "title": "t", "head": "f", "base": "m"}, operations.Env{})
	b, _ := PullCreate(map[string]any{"repo": "o/n", "title": "other", "head": "f", "base": "m"}, operations.Env{})
	ra, rb := a.Requirements()[0], b.Requirements()[0]
	if ra.Action != "pull.create" || ra.Resource.ID != "o/n" {
		t.Fatalf("unexpected requirement %+v", ra)
	}
	if ra.Context["content_hash"] == "" || ra.Context["content_hash"] == rb.Context["content_hash"] {
		t.Fatalf("content hash should differ per content: %q vs %q", ra.Context["content_hash"], rb.Context["content_hash"])
	}
}

// TestPullCreateDescribe checks the pull-create description names the repo, title and branches.
func TestPullCreateDescribe(t *testing.T) {
	op, _ := PullCreate(map[string]any{"repo": "o/n", "title": "Add feature", "head": "feature", "base": "main"}, operations.Env{})
	d := op.Describe()
	if !strings.Contains(d, "o/n") || !strings.Contains(d, "Add feature") || !strings.Contains(d, "feature") || !strings.Contains(d, "main") {
		t.Fatalf("describe missing repo/title/branches: %q", d)
	}
}

// TestPullCreateExecutePosts checks executing a pull create POSTs to the repo's pulls endpoint.
func TestPullCreateExecutePosts(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/o/n/pulls" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var payload map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&payload)
		if payload["title"] != "t" || payload["head"] != "f" || payload["base"] != "m" || payload["draft"] != true {
			t.Errorf("unexpected payload: %v", payload)
		}
		_, _ = w.Write([]byte(`{"number":12,"html_url":"u"}`))
	}))
	defer stub.Close()
	old := apiBaseURL
	apiBaseURL = stub.URL
	defer func() { apiBaseURL = old }()

	op, _ := PullCreate(map[string]any{"repo": "o/n", "title": "t", "head": "f", "base": "m", "draft": true}, operations.Env{GitHubToken: "tok"})
	result, err := op.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if asNumber(result["number"]) != 12 {
		t.Fatalf("unexpected result: %v", result)
	}
}

// asNumber reads a JSON number from a decoded map as an int for assertions.
func asNumber(v any) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}
