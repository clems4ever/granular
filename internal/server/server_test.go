package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/authz"
	"github.com/clems4ever/granular/internal/grants"
	"github.com/clems4ever/granular/internal/operations"
)

// fakeOp is a no-network operation used to exercise the server.
type fakeOp struct{}

// Type returns the fake operation type id.
func (fakeOp) Type() string { return "test.op" }

// Describe returns the fake operation summary.
func (fakeOp) Describe() string { return "a test operation" }

// Requirements returns the fake operation authorization requirements.
func (fakeOp) Requirements() []authz.Requirement {
	return []authz.Requirement{{Action: "issue.view", Resource: authz.RepoRef("o/n")}}
}

// Execute returns a fixed success result.
func (fakeOp) Execute(ctx context.Context) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

// testServer builds an httptest server backed by a temp store and a fake operation.
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

// TestOperationPendingThenApprovedThenCompleted drives an operation request from pending through approval to completion.
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
	sr, _ := http.Get(ts.URL + "/api/grant-requests/" + id)
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

// TestOperationUnknownTypeIsBadRequest checks an unregistered operation type returns 400.
func TestOperationUnknownTypeIsBadRequest(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"nope"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// TestGrantRequestWithoutCapabilities checks a grant request with an empty capability bundle returns 400.
func TestGrantRequestWithoutCapabilities(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Post(ts.URL+"/api/grant-requests", "application/json", strings.NewReader(`{"reason":"nothing"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for an empty capability bundle, got %d", resp.StatusCode)
	}
}

// TestRequestStatusNotFound checks the status endpoint 404s for an unknown request id.
func TestRequestStatusNotFound(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Get(ts.URL + "/api/grant-requests/missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// TestApprovePageRendersForm checks the approval page renders the request's form.
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

// TestApprovePageNotFound checks the approval page 404s for an unknown id.
func TestApprovePageNotFound(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Get(ts.URL + "/approve/missing")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// TestApproveSubmitReject checks rejecting via the approval form sets the rejected status.
func TestApproveSubmitReject(t *testing.T) {
	ts := testServer(t)
	resp, _ := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"test.op"}`))
	id := decode(t, resp)["request_id"].(string)

	form := url.Values{"decision": {"reject"}}
	rr, _ := http.PostForm(ts.URL+"/approve/"+id, form)
	if rr.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.StatusCode)
	}
	sr, _ := http.Get(ts.URL + "/api/grant-requests/" + id)
	if decode(t, sr)["status"] != "rejected" {
		t.Fatalf("status should be rejected")
	}
}

// TestGitProxyDeniesWithoutGrant checks the git proxy returns 403 for clone traffic without a grant.
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

// TestGitProxyDeniesPushWithoutGrant checks the git proxy returns 403 for push traffic without a grant.
func TestGitProxyDeniesPushWithoutGrant(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/git/owner/name.git/info/refs?service=git-receive-pack")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 for push without grant, got %d", resp.StatusCode)
	}
	if !strings.Contains(readBody(t, resp), "push") {
		t.Fatalf("expected a push-related message")
	}
}

// TestRepoFromGitPath checks repoFromGitPath parses owner/name from a smart-HTTP path.
func TestRepoFromGitPath(t *testing.T) {
	repo, ok := repoFromGitPath("owner/name.git/info/refs")
	if !ok || repo != "owner/name" {
		t.Fatalf("repoFromGitPath = %q, %v", repo, ok)
	}
	if _, ok := repoFromGitPath("solo"); ok {
		t.Fatalf("expected failure for single-segment path")
	}
}

// TestPermissionsRequestFlow checks a capability request, once approved, authorizes a covered operation.
func TestPermissionsRequestFlow(t *testing.T) {
	ts := testServer(t)

	// Request a broad capability that covers the fake op's issue.view requirement.
	body := `{"reason":"work","capabilities":[{"actions":["issues.read"],"resource":{"type":"github.repo","match":{"owner":"o","name":"n"}}}]}`
	resp, err := http.Post(ts.URL+"/api/grant-requests", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("want 202, got %d", resp.StatusCode)
	}
	id, _ := decode(t, resp)["request_id"].(string)
	if id == "" {
		t.Fatal("missing request_id")
	}

	// The approval page should show the granted Cedar policies.
	page, _ := http.Get(ts.URL + "/approve/" + id)
	if html := readBody(t, page); !strings.Contains(html, "permit") || !strings.Contains(html, "issues.read") {
		t.Fatalf("approval page should show the policies")
	}

	// Approve it.
	if ar, _ := http.PostForm(ts.URL+"/approve/"+id, url.Values{"decision": {"approve"}, "ttl": {"1h"}}); ar.StatusCode != http.StatusOK {
		t.Fatalf("approve failed: %d", ar.StatusCode)
	}

	// Now the operation (issue.view on repo o/n) is authorized by the broad grant.
	op, _ := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"test.op"}`))
	if op.StatusCode != http.StatusOK {
		t.Fatalf("operation should be authorized after the permissions grant, got %d", op.StatusCode)
	}
	if decode(t, op)["status"] != "completed" {
		t.Fatal("operation should be completed")
	}
}

// TestPermissionsRequestRejectsUnknownAction checks a capability request with an unknown action returns 400.
func TestPermissionsRequestRejectsUnknownAction(t *testing.T) {
	ts := testServer(t)
	body := `{"capabilities":[{"actions":["issue.delete"],"resource":{"type":"github.repo","match":{"owner":"o","name":"n"}}}]}`
	resp, _ := http.Post(ts.URL+"/api/grant-requests", "application/json", strings.NewReader(body))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown action, got %d", resp.StatusCode)
	}
}

// TestIndexPageRenders checks the landing page renders.
func TestIndexPageRenders(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if html := readBody(t, resp); !strings.Contains(html, "granular") || !strings.Contains(html, "/static/style.css") {
		t.Fatalf("index page missing expected content")
	}
}

// TestCatalogPageRenders checks the catalog page renders its key content.
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
	for _, want := range []string{"Capability catalog", "github.issue", "issue.view", "granular github issue view"} {
		if !strings.Contains(html, want) {
			t.Errorf("catalog page missing %q", want)
		}
	}
}

// TestCatalogJSON checks the JSON catalog endpoint returns resources and actions.
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

// TestWebPagesRequireAuthWhenEnabled checks that, with auth enabled, web pages redirect to login while the agent API stays open.
func TestWebPagesRequireAuthWhenEnabled(t *testing.T) {
	reg := operations.NewRegistry()
	store, err := grants.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := New(reg, store, operations.Env{}, "http://example.test")
	srv.UseAuth(NewAuthenticator(AuthConfig{
		ClientID: "c", ClientSecret: "s", AllowedUsers: []string{"octocat"},
		SessionSecret: []byte("test-secret-key-0123456789abcdef"), BaseURL: "http://example.test",
	}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// A human page redirects to the GitHub login.
	page, _ := client.Get(ts.URL + "/grants")
	if page.StatusCode != http.StatusFound || !strings.HasPrefix(page.Header.Get("Location"), "/auth/login") {
		t.Fatalf("grants page should redirect to login, got %d %q", page.StatusCode, page.Header.Get("Location"))
	}
	// The agent-facing API is not behind the browser login.
	apiResp, _ := client.Get(ts.URL + "/api/catalog")
	if apiResp.StatusCode != http.StatusOK {
		t.Fatalf("api/catalog should stay open, got %d", apiResp.StatusCode)
	}
}

// TestParseTTLFallsBack checks parseTTL defaults to two minutes for empty or invalid input.
func TestParseTTLFallsBack(t *testing.T) {
	if parseTTL("").Minutes() != 2 {
		t.Errorf("empty should default to 2m")
	}
	if parseTTL("garbage").Minutes() != 2 {
		t.Errorf("invalid should default to 2m")
	}
	if parseTTL("15m").Minutes() != 15 {
		t.Errorf("15m should parse")
	}
}
