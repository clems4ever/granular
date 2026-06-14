package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/grants"
	"github.com/clems4ever/granular/internal/operations"
)

// fakeOp is a no-network operation used to exercise the server.
type fakeOp struct{}

func (fakeOp) Type() string          { return "test.op" }
func (fakeOp) PermissionKey() string { return "test.op:x" }
func (fakeOp) Describe() string      { return "a test operation" }
func (fakeOp) Execute(ctx context.Context) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg := operations.NewRegistry()
	reg.Register("test.op", func(map[string]any, operations.Env) (operations.Operation, error) {
		return fakeOp{}, nil
	})
	store, err := grants.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := New(reg, store, operations.Env{}, "http://example.test")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestOperationPendingThenApprovedThenCompleted(t *testing.T) {
	ts := testServer(t)

	// First attempt -> pending.
	resp, err := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"test.op"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("want 202, got %d", resp.StatusCode)
	}
	body := decode(t, resp)
	id, _ := body["request_id"].(string)
	if id == "" {
		t.Fatal("missing request_id")
	}

	// Approve.
	form := url.Values{"decision": {"approve"}, "ttl": {"1h"}}
	ar, err := http.PostForm(ts.URL+"/approve/"+id, form)
	if err != nil {
		t.Fatal(err)
	}
	if ar.StatusCode != http.StatusOK {
		t.Fatalf("approve: want 200, got %d", ar.StatusCode)
	}

	// Status should be approved.
	sr, _ := http.Get(ts.URL + "/api/requests/" + id)
	sb := decode(t, sr)
	if sb["status"] != "approved" {
		t.Fatalf("want approved, got %v", sb["status"])
	}

	// Retry -> completed.
	resp2, _ := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"test.op"}`))
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry: want 200, got %d", resp2.StatusCode)
	}
	rb := decode(t, resp2)
	if rb["status"] != "completed" {
		t.Fatalf("want completed, got %v", rb["status"])
	}
}

func TestOperationUnknownTypeIsBadRequest(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"nope"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestRequestStatusNotFound(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Get(ts.URL + "/api/requests/missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestApprovePageRendersForm(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"test.op"}`))
	id := decode(t, resp)["request_id"].(string)

	page, _ := http.Get(ts.URL + "/approve/" + id)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", page.StatusCode)
	}
	html := readBody(t, page)
	if !strings.Contains(html, "a test operation") || !strings.Contains(html, "Approve") {
		t.Fatalf("approval page missing expected content")
	}
}

func TestApprovePageNotFound(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Get(ts.URL + "/approve/missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestApproveSubmitReject(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"test.op"}`))
	id := decode(t, resp)["request_id"].(string)

	form := url.Values{"decision": {"reject"}}
	rr, _ := http.PostForm(ts.URL+"/approve/"+id, form)
	if rr.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.StatusCode)
	}
	sr, _ := http.Get(ts.URL + "/api/requests/" + id)
	if decode(t, sr)["status"] != "rejected" {
		t.Fatalf("status should be rejected")
	}
}

func TestGitProxyDeniesWithoutGrant(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/git/owner/name.git/info/refs?service=git-upload-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 without grant, got %d", resp.StatusCode)
	}
}

func TestGitProxyRejectsPush(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/git/owner/name.git/info/refs?service=git-receive-pack")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 for push, got %d", resp.StatusCode)
	}
	if !strings.Contains(readBody(t, resp), "push") {
		t.Fatalf("expected a push-related message")
	}
}

func TestRepoFromGitPath(t *testing.T) {
	repo, ok := repoFromGitPath("owner/name.git/info/refs")
	if !ok || repo != "owner/name" {
		t.Fatalf("repoFromGitPath = %q, %v", repo, ok)
	}
	if _, ok := repoFromGitPath("solo"); ok {
		t.Fatalf("expected failure for single-segment path")
	}
}

func TestCatalogPageRenders(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/catalog")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	html := readBody(t, resp)
	for _, want := range []string{"capability catalog", "github.issue", "issue.view", "granular github issue view"} {
		if !strings.Contains(html, want) {
			t.Errorf("catalog page missing %q", want)
		}
	}
}

func TestCatalogJSON(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/api/catalog")
	if err != nil {
		t.Fatal(err)
	}
	body := decode(t, resp)
	if body["resources"] == nil || body["actions"] == nil {
		t.Fatalf("catalog JSON missing resources/actions: %v", body)
	}
}

func TestParseTTLFallsBack(t *testing.T) {
	if parseTTL("").Hours() != 1 {
		t.Errorf("empty should default to 1h")
	}
	if parseTTL("garbage").Hours() != 1 {
		t.Errorf("invalid should default to 1h")
	}
	if parseTTL("15m").Minutes() != 15 {
		t.Errorf("15m should parse")
	}
}
