package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clems4ever/granular/auth_server/store"
)

// TestParseTTLFallsBack checks empty and invalid durations default to 2 minutes.
func TestParseTTLFallsBack(t *testing.T) {
	if parseTTL("") != defaultTTL || parseTTL("nonsense") != defaultTTL || parseTTL("-1h") != defaultTTL {
		t.Fatal("invalid/empty values should fall back to defaultTTL")
	}
	if parseTTL("1h") != time.Hour {
		t.Fatal("valid duration not parsed")
	}
}

// TestHumanizeUntil renders relative time-to-expiry across hours, minutes, seconds and
// the already-expired case.
func TestHumanizeUntil(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	cases := map[time.Duration]string{
		2*time.Hour + 5*time.Minute: "in 2h 5m",
		12 * time.Minute:            "in 12m",
		30 * time.Second:            "in 30s",
		-time.Minute:                "expired",
		0:                           "expired",
	}
	for d, want := range cases {
		if got := humanizeUntil(now, now.Add(d)); got != want {
			t.Fatalf("humanizeUntil(+%s) = %q, want %q", d, got, want)
		}
	}
}

// TestApprovePageRendersItems renders a pending proposal's items verbatim.
func TestApprovePageRendersItems(t *testing.T) {
	_, h := newServer(t)
	token := createPolicy(t, h)
	id := propose(t, h, token, "me@example.com")

	resp := do(t, h, http.MethodGet, "/proposal/"+id, nil, "", false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /proposal/{id} = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "View repo r") {
		t.Fatal("consent page missing the item summary")
	}
}

// TestApprovePageNotFound returns 404 for an unknown proposal id.
func TestApprovePageNotFound(t *testing.T) {
	_, h := newServer(t)
	resp := do(t, h, http.MethodGet, "/proposal/nope", nil, "", false)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestApproveSubmitReject records a rejection and marks the proposal rejected.
func TestApproveSubmitReject(t *testing.T) {
	srv, h := newServer(t)
	token := createPolicy(t, h)
	id := propose(t, h, token, "me@example.com")

	ts := httptest.NewServer(h)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/proposal/"+id, strings.NewReader("decision=reject"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject = %d, want 200", resp.StatusCode)
	}
	p, err := srv.store.GetProposal(id)
	if err != nil || p.Status != store.StatusRejected {
		t.Fatalf("proposal status = %v (err %v), want rejected", p.Status, err)
	}
}

// TestApproveDeniesWrongApprover blocks a signed-in user who is not the named approver.
func TestApproveDeniesWrongApprover(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "as.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	srv := New(st, "http://as.example", map[string]string{"gw": gwSecret})
	srv.UseAdminToken(adminToken)
	auth := NewAuthenticator(AuthConfig{ClientID: "id", ClientSecret: "sec", SessionSecret: []byte("k"), BaseURL: "http://as.example"})
	srv.UseAuth(auth)
	h := srv.Handler()

	token := createPolicy(t, h)
	id := propose(t, h, token, "me@example.com")

	ts := httptest.NewServer(h)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/proposal/"+id, nil)
	req.AddCookie(auth.sessionCookieFor("someone-else@example.com"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a non-approver", resp.StatusCode)
	}
}
