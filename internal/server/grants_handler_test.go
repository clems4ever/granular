package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/clems4ever/granular/internal/api"
)

// approveAGrant submits a permissions request and approves it, returning the
// active grant id for use by the grants/revoke tests.
func approveAGrant(t *testing.T, ts string) string {
	t.Helper()
	body := `{"reason":"work","capabilities":[{"actions":["issues.read"],"resource":{"type":"github.repo","match":{"owner":"o","name":"n"}}}]}`
	resp, err := http.Post(ts+"/api/permissions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := decode(t, resp)["request_id"].(string)
	if id == "" {
		t.Fatal("missing request_id")
	}
	if ar, _ := http.PostForm(ts+"/approve/"+id, url.Values{"decision": {"approve"}, "ttl": {"1h"}}); ar.StatusCode != http.StatusOK {
		t.Fatalf("approve failed: %d", ar.StatusCode)
	}
	return id
}

func grantsResponse(t *testing.T, ts string) api.GrantsResponse {
	t.Helper()
	resp, err := http.Get(ts + "/api/grants")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out api.GrantsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode grants: %v", err)
	}
	return out
}

func TestGrantsPageRenders(t *testing.T) {
	ts := testServer(t)
	resp, err := http.Get(ts.URL + "/grants")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	if html := readBody(t, resp); !strings.Contains(html, "Active grants") || !strings.Contains(html, "Request history") {
		t.Fatalf("grants page missing expected sections")
	}
}

func TestGrantsPageShowsExpiryAndRevoke(t *testing.T) {
	ts := testServer(t)
	reqID := approveAGrant(t, ts.URL)
	grant := grantsResponse(t, ts.URL).Grants[0]

	resp, _ := http.Get(ts.URL + "/grants")
	html := readBody(t, resp)

	// The active grant exposes a revoke form and an absolute expiry date.
	if !strings.Contains(html, `action="/grants/`+grant.ID+`/revoke"`) {
		t.Fatalf("grant revoke form missing")
	}
	if !strings.Contains(html, time.Now().Format("2006-01-02")) {
		t.Fatalf("expiry date not shown on the page")
	}
	// The (approved) request row is revocable, so it also has a revoke form.
	if !strings.Contains(html, `action="/grants/`+reqID+`/revoke"`) {
		t.Fatalf("request revoke form missing")
	}
}

func TestGrantsJSONListsActiveGrants(t *testing.T) {
	ts := testServer(t)
	reqID := approveAGrant(t, ts.URL)

	got := grantsResponse(t, ts.URL)
	if len(got.Grants) != 1 {
		t.Fatalf("want 1 active grant, got %d", len(got.Grants))
	}
	if got.Grants[0].RequestID != reqID {
		t.Fatalf("grant should link to request %s, got %s", reqID, got.Grants[0].RequestID)
	}
	found := false
	for _, r := range got.Requests {
		if r.ID == reqID {
			found = true
		}
	}
	if !found {
		t.Fatalf("request %s missing from history", reqID)
	}
}

func TestRevokeEndpointRemovesGrant(t *testing.T) {
	ts := testServer(t)
	approveAGrant(t, ts.URL)
	grant := grantsResponse(t, ts.URL).Grants[0]

	resp, err := http.Post(ts.URL+"/api/grants/"+grant.ID+"/revoke", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var rr api.RevokeResponse
	_ = json.NewDecoder(resp.Body).Decode(&rr)
	if resp.StatusCode != http.StatusOK || rr.Revoked != 1 {
		t.Fatalf("revoke = status %d, revoked %d", resp.StatusCode, rr.Revoked)
	}
	if after := grantsResponse(t, ts.URL); len(after.Grants) != 0 {
		t.Fatalf("grant should be gone, got %d", len(after.Grants))
	}

	// Revoking an unknown id is a 404.
	miss, _ := http.Post(ts.URL+"/api/grants/nope/revoke", "application/json", nil)
	if miss.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404 for unknown id, got %d", miss.StatusCode)
	}
}

func TestRevokePendingRequestEndpoint(t *testing.T) {
	ts := testServer(t)

	// A pending operation request (no grant exists for it yet).
	resp, err := http.Post(ts.URL+"/api/operations", "application/json", strings.NewReader(`{"type":"test.op"}`))
	if err != nil {
		t.Fatal(err)
	}
	reqID, _ := decode(t, resp)["request_id"].(string)
	if reqID == "" {
		t.Fatal("missing request_id")
	}

	// Revoking it by request id succeeds (200) even with zero active grants.
	rv, err := http.Post(ts.URL+"/api/grants/"+reqID+"/revoke", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rv.StatusCode != http.StatusOK {
		t.Fatalf("want 200 revoking a pending request, got %d", rv.StatusCode)
	}

	// The request is now marked revoked.
	sr, _ := http.Get(ts.URL + "/api/requests/" + reqID)
	if got := decode(t, sr)["status"]; got != "revoked" {
		t.Fatalf("request should be revoked, got %v", got)
	}
}

func TestRevokeFormRedirects(t *testing.T) {
	ts := testServer(t)
	approveAGrant(t, ts.URL)
	grant := grantsResponse(t, ts.URL).Grants[0]

	// Do not follow redirects so we can assert the 303.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Post(ts.URL+"/grants/"+grant.ID+"/revoke", "application/x-www-form-urlencoded", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("want 303, got %d", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/grants" {
		t.Fatalf("want redirect to /grants, got %q", loc)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("one\ntwo\nthree"); got != "one" {
		t.Fatalf("firstLine = %q", got)
	}
	if got := firstLine("single"); got != "single" {
		t.Fatalf("firstLine = %q", got)
	}
}

func TestHumanRemaining(t *testing.T) {
	if got := humanRemaining(90 * time.Second); got != "1m30s" {
		t.Fatalf("humanRemaining = %q", got)
	}
	if got := humanRemaining(-time.Second); got != "expired" {
		t.Fatalf("negative should be expired, got %q", got)
	}
}
