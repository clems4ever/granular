package resourceservergithub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRESTProxyRefinedRouteMapsToAction checks a known endpoint resolves to its precise
// action, resource and a body-bound context, and is forwarded to the upstream with the PAT
// as a Bearer token and the body intact.
func TestRESTProxyRefinedRouteMapsToAction(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotAuth, gotBody = r.URL.Path, r.Header.Get("Authorization"), string(b)
		io.WriteString(w, "{}")
	}))
	defer upstream.Close()

	auth := &stubAuthorizer{allow: true}
	p := newRESTProxy("PATSECRET", upstream.URL, auth)
	body := `{"event":"APPROVE"}`
	r := httptest.NewRequest(http.MethodPost, "/api/github/repos/clems4ever/granular/pulls/7/reviews", strings.NewReader(body))
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if auth.token != "subtok" {
		t.Fatalf("authorizer saw token %q, want subtok", auth.token)
	}
	if len(auth.reqs) != 1 {
		t.Fatalf("requirements = %+v, want one", auth.reqs)
	}
	got := auth.reqs[0]
	if got.Action != "pull.review" {
		t.Fatalf("action = %q, want pull.review", got.Action)
	}
	if got.Resource.Type != "github.pull" || got.Resource.ID != "clems4ever/granular#7" {
		t.Fatalf("resource = %+v, want github.pull clems4ever/granular#7", got.Resource)
	}
	if got.Context["body_sha256"] == "" {
		t.Fatal("expected the requirement to be content-bound to the request body")
	}
	if got.Resource.Parent == nil || got.Resource.Parent.Type != "github.repo" {
		t.Fatalf("resource parent = %+v, want a github.repo", got.Resource.Parent)
	}
	// Forwarded correctly: prefix stripped, PAT as Bearer, body preserved, token not leaked.
	if gotPath != "/repos/clems4ever/granular/pulls/7/reviews" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotAuth != "Bearer PATSECRET" {
		t.Fatalf("upstream auth = %q, want Bearer PATSECRET", gotAuth)
	}
	if gotBody != body {
		t.Fatalf("upstream body = %q, want %q", gotBody, body)
	}
}

// TestRESTProxyDefaultMapping checks an endpoint absent from the refinement table still
// fails closed: a GET is gated by github.read and a mutating method by github.write, each
// scoped to the resource named by the path.
func TestRESTProxyDefaultMapping(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{}")
	}))
	defer upstream.Close()

	cases := []struct {
		method, path string
		wantAction   string
		wantResType  string
		wantResID    string
	}{
		{http.MethodGet, "/api/github/repos/o/r/contents/x", "github.read", "github.repo", "o/r"},
		{http.MethodDelete, "/api/github/repos/o/r/git/refs/heads/main", "github.write", "github.repo", "o/r"},
		{http.MethodGet, "/api/github/orgs/acme/members", "github.read", "github.org", "acme"},
		{http.MethodGet, "/api/github/notifications", "github.read", "github.api", "github"},
	}
	for _, c := range cases {
		auth := &stubAuthorizer{allow: true}
		p := newRESTProxy("pat", upstream.URL, auth)
		r := httptest.NewRequest(c.method, c.path, nil)
		r.Header.Set("Authorization", basicHeader("granular", "subtok"))
		p.ServeHTTP(httptest.NewRecorder(), r)
		if len(auth.reqs) != 1 {
			t.Fatalf("%s %s: requirements = %+v, want one", c.method, c.path, auth.reqs)
		}
		got := auth.reqs[0]
		if got.Action != c.wantAction {
			t.Fatalf("%s %s: action = %q, want %q", c.method, c.path, got.Action, c.wantAction)
		}
		if got.Resource.Type != c.wantResType || got.Resource.ID != c.wantResID {
			t.Fatalf("%s %s: resource = %+v, want %s %s", c.method, c.path, got.Resource, c.wantResType, c.wantResID)
		}
	}
}

// TestRESTProxyDeniesUnauthorized checks a denied subject gets 403 and the request is not
// forwarded.
func TestRESTProxyDeniesUnauthorized(t *testing.T) {
	var forwarded bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = true
	}))
	defer upstream.Close()

	p := newRESTProxy("pat", upstream.URL, &stubAuthorizer{allow: false})
	r := httptest.NewRequest(http.MethodPost, "/api/github/repos/o/r/issues", strings.NewReader(`{"title":"x"}`))
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if forwarded {
		t.Fatal("denied request must not be forwarded upstream")
	}
}

// TestRESTProxyRequiresToken checks an unauthenticated request is challenged with 401.
func TestRESTProxyRequiresToken(t *testing.T) {
	p := newRESTProxy("pat", "http://upstream.invalid", &stubAuthorizer{allow: true})
	r := httptest.NewRequest(http.MethodGet, "/api/github/repos/o/r/pulls", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if !strings.Contains(w.Header().Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("WWW-Authenticate = %q, want a Basic challenge", w.Header().Get("WWW-Authenticate"))
	}
}

// TestRESTProxyForwardsWithPAT checks an allowed request is forwarded with the prefix
// stripped, the PAT as a Bearer token, and the client's subject token not leaked.
func TestRESTProxyForwardsWithPAT(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		io.WriteString(w, "[]")
	}))
	defer upstream.Close()

	p := newRESTProxy("PATSECRET", upstream.URL, &stubAuthorizer{allow: true})
	r := httptest.NewRequest(http.MethodGet, "/api/github/repos/o/r/pulls", nil)
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotPath != "/repos/o/r/pulls" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotAuth != "Bearer PATSECRET" {
		t.Fatalf("upstream auth = %q, want Bearer PATSECRET", gotAuth)
	}
	if strings.Contains(gotAuth, "subtok") {
		t.Fatalf("subject token leaked upstream: %q", gotAuth)
	}
}

// TestRESTProxyContentBindingDistinguishesBodies checks two different review bodies produce
// different content hashes, so a grant for one body does not authorize another.
func TestRESTProxyContentBindingDistinguishesBodies(t *testing.T) {
	hashFor := func(body string) string {
		auth := &stubAuthorizer{allow: true}
		p := newRESTProxy("pat", "http://upstream.invalid", auth)
		r := httptest.NewRequest(http.MethodPost, "/api/github/repos/o/r/pulls/1/reviews", strings.NewReader(body))
		r.Header.Set("Authorization", basicHeader("granular", "subtok"))
		// upstream is unreachable, but authorization (and thus requirement building) runs first;
		// deny would 403 — we allow, so it tries to forward and fails after recording reqs.
		p.ServeHTTP(httptest.NewRecorder(), r)
		if len(auth.reqs) != 1 {
			t.Fatalf("requirements = %+v, want one", auth.reqs)
		}
		return auth.reqs[0].Context["body_sha256"]
	}
	if a, b := hashFor(`{"event":"APPROVE"}`), hashFor(`{"event":"REQUEST_CHANGES"}`); a == "" || a == b {
		t.Fatalf("expected distinct non-empty body hashes, got %q and %q", a, b)
	}
}
