package resourceservergithub

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/resourceserver"
)

// stubAuthorizer records the last Authorize call and returns a fixed decision.
type stubAuthorizer struct {
	allow bool
	err   error
	token string
	reqs  []resourceserver.Requirement
}

// Authorize records its inputs and returns the stub's configured decision.
//
// @arg ctx Context (unused).
// @arg token The subject token to record.
// @arg reqs The requirements to record.
// @return bool The configured decision.
// @error error The configured error.
//
// @testcase TestGitProxyForwardsWithPAT drives an allowing stub.
func (s *stubAuthorizer) Authorize(ctx context.Context, token string, reqs []resourceserver.Requirement) (bool, error) {
	s.token, s.reqs = token, reqs
	return s.allow, s.err
}

// basicHeader builds an HTTP Basic Authorization header value for user:pass.
//
// @arg user The basic-auth username.
// @arg pass The basic-auth password.
// @return string The "Basic <base64>" header value.
//
// @testcase TestGitProxyDeniesUnauthorized sends credentials built here.
func basicHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// TestGitProxyRequiresToken checks an unauthenticated request is challenged with 401.
func TestGitProxyRequiresToken(t *testing.T) {
	p := newGitProxy("pat", "http://upstream.invalid", &stubAuthorizer{allow: true})
	r := httptest.NewRequest(http.MethodGet, "/git/clems4ever/granular.git/info/refs?service=git-upload-pack", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Fatalf("WWW-Authenticate = %q, want a Basic challenge", got)
	}
}

// TestGitProxyDeniesUnauthorized checks a denied subject gets 403 and is not forwarded.
func TestGitProxyDeniesUnauthorized(t *testing.T) {
	auth := &stubAuthorizer{allow: false}
	p := newGitProxy("pat", "http://upstream.invalid", auth)
	r := httptest.NewRequest(http.MethodGet, "/git/clems4ever/granular.git/info/refs?service=git-upload-pack", nil)
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if auth.token != "subtok" {
		t.Fatalf("authorizer saw token %q, want subtok", auth.token)
	}
	if len(auth.reqs) != 1 || auth.reqs[0].Action != "repo.clone" {
		t.Fatalf("requirement = %+v, want a single repo.clone", auth.reqs)
	}
}

// TestGitProxyForwardsWithPAT checks an allowed request is forwarded to the upstream with
// the rewritten path and the server PAT, and the client's subject token is not leaked.
func TestGitProxyForwardsWithPAT(t *testing.T) {
	var gotPath, gotAuth, gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Model a private repo: the anonymous public-repo probe is rejected, so the proxy
		// falls through to authorization and forwards with the PAT.
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		gotPath, gotAuth, gotQuery = r.URL.Path, r.Header.Get("Authorization"), r.URL.RawQuery
		io.WriteString(w, "refs")
	}))
	defer upstream.Close()

	auth := &stubAuthorizer{allow: true}
	p := newGitProxy("PATSECRET", upstream.URL, auth)
	r := httptest.NewRequest(http.MethodGet, "/git/clems4ever/granular.git/info/refs?service=git-upload-pack", nil)
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotPath != "/clems4ever/granular.git/info/refs" {
		t.Fatalf("upstream path = %q", gotPath)
	}
	if gotQuery != "service=git-upload-pack" {
		t.Fatalf("upstream query = %q", gotQuery)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:PATSECRET"))
	if gotAuth != want {
		t.Fatalf("upstream auth = %q, want PAT credential", gotAuth)
	}
	if strings.Contains(gotAuth, "subtok") {
		t.Fatalf("subject token leaked upstream: %q", gotAuth)
	}
}

// TestGitProxyForwardsAnonymouslyWithoutPAT checks that when no PAT is configured an
// authorized request is forwarded with no Authorization header at all, rather than an empty
// "x-access-token:" credential. GitHub rejects an invalid credential with 401 even for a
// public repo, so injecting one would turn a clone into an auth error. The repo here is
// private (the anonymous probe is rejected) so the public-repo bypass does not apply and the
// empty-PAT path in direct is exercised in isolation.
func TestGitProxyForwardsAnonymouslyWithoutPAT(t *testing.T) {
	var gotAuth string
	var hadAuth bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, hadAuth = r.Header["Authorization"]
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized) // private: no anonymous access
		}
	}))
	defer upstream.Close()

	p := newGitProxy("", upstream.URL, &stubAuthorizer{allow: true})
	r := httptest.NewRequest(http.MethodGet, "/git/clems4ever/granular.git/info/refs?service=git-upload-pack", nil)
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if hadAuth {
		t.Fatalf("upstream received Authorization %q, want none (no PAT to inject)", gotAuth)
	}
}

// TestGitProxyServesPublicRepoWithoutGrant is the regression test for a public repo being
// blocked: when the upstream advertises the repo's refs anonymously, a clone is forwarded
// without consulting the AS and without any credentials — even though the authorizer would
// deny it — so a public repo is never blocked for lack of a grant.
func TestGitProxyServesPublicRepoWithoutGrant(t *testing.T) {
	var hadAuth bool
	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if _, ok := r.Header["Authorization"]; ok {
			hadAuth = true
		}
		io.WriteString(w, "refs") // public: anonymous access succeeds
	}))
	defer upstream.Close()

	auth := &stubAuthorizer{allow: false} // AS would deny; it must not be consulted
	p := newGitProxy("PATSECRET", upstream.URL, auth)
	r := httptest.NewRequest(http.MethodGet, "/git/clems4ever/llmbox.git/info/refs?service=git-upload-pack", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a public repo", w.Code)
	}
	if auth.token != "" || auth.reqs != nil {
		t.Fatalf("authorizer was consulted (%q, %+v); a public read must not be", auth.token, auth.reqs)
	}
	if hadAuth {
		t.Fatal("a public-repo read leaked credentials upstream")
	}
	if hits == 0 {
		t.Fatal("request was not forwarded to the upstream")
	}
}

// TestGitProxyPrivateRepoStillAuthorizes checks a private repo (anonymous probe rejected) is
// still gated: a denied subject gets 403 and the authorizer is consulted.
func TestGitProxyPrivateRepoStillAuthorizes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized) // private
		}
	}))
	defer upstream.Close()

	auth := &stubAuthorizer{allow: false}
	p := newGitProxy("PATSECRET", upstream.URL, auth)
	r := httptest.NewRequest(http.MethodGet, "/git/clems4ever/secret.git/info/refs?service=git-upload-pack", nil)
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a denied private repo", w.Code)
	}
	if auth.token != "subtok" {
		t.Fatalf("authorizer saw token %q, want subtok", auth.token)
	}
}

// TestGitProxyPushAlwaysAuthorizes checks a push is gated even when the repo is publicly
// readable: the public-repo bypass is read-only, so receive-pack still requires a grant.
func TestGitProxyPushAlwaysAuthorizes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "refs") // public for reads
	}))
	defer upstream.Close()

	auth := &stubAuthorizer{allow: false}
	p := newGitProxy("PATSECRET", upstream.URL, auth)
	r := httptest.NewRequest(http.MethodGet, "/git/clems4ever/llmbox.git/info/refs?service=git-receive-pack", nil)
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (a push must be authorized even on a public repo)", w.Code)
	}
	if len(auth.reqs) != 1 || auth.reqs[0].Action != "repo.push" {
		t.Fatalf("requirement = %+v, want a single repo.push", auth.reqs)
	}
}

// TestGitProxyRejectsBadPath checks a path with no .git/ segment is a 400.
func TestGitProxyRejectsBadPath(t *testing.T) {
	p := newGitProxy("pat", "http://upstream.invalid", &stubAuthorizer{allow: true})
	r := httptest.NewRequest(http.MethodGet, "/git/not-a-repo/info/refs", nil)
	r.Header.Set("Authorization", basicHeader("granular", "subtok"))
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestParseGitRequest checks info/refs and the RPC endpoints parse to repo + service.
func TestParseGitRequest(t *testing.T) {
	cases := []struct{ path, query, repo, service string }{
		{"/git/o/r.git/info/refs", "service=git-receive-pack", "o/r", "git-receive-pack"},
		{"/git/o/r.git/git-upload-pack", "", "o/r", "git-upload-pack"},
		{"/git/o/r.git/git-receive-pack", "", "o/r", "git-receive-pack"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, c.path+"?"+c.query, nil)
		repo, service, err := parseGitRequest(r)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if repo != c.repo || service != c.service {
			t.Fatalf("%s: got (%q,%q), want (%q,%q)", c.path, repo, service, c.repo, c.service)
		}
	}
	if _, _, err := parseGitRequest(httptest.NewRequest(http.MethodGet, "/git/o/r.git/objects/foo", nil)); err == nil {
		t.Fatal("expected error for unsupported git path")
	}
}

// TestServiceAction checks the service-to-action mapping.
func TestServiceAction(t *testing.T) {
	if a, ok := serviceAction("git-upload-pack"); !ok || a != "repo.clone" {
		t.Fatalf("upload-pack -> (%q,%v)", a, ok)
	}
	if a, ok := serviceAction("git-receive-pack"); !ok || a != "repo.push" {
		t.Fatalf("receive-pack -> (%q,%v)", a, ok)
	}
	if _, ok := serviceAction("git-bogus"); ok {
		t.Fatal("unknown service should not map")
	}
}

// TestSubjectTokenFromBasicAuth checks the token is read from the password, else username.
func TestSubjectTokenFromBasicAuth(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/git/o/r.git/info/refs", nil)
	r.Header.Set("Authorization", basicHeader("granular", "frompass"))
	if got := subjectToken(r); got != "frompass" {
		t.Fatalf("token = %q, want frompass", got)
	}
	r.Header.Set("Authorization", basicHeader("fromuser", ""))
	if got := subjectToken(r); got != "fromuser" {
		t.Fatalf("token = %q, want fromuser", got)
	}
	r.Header.Del("Authorization")
	if got := subjectToken(r); got != "" {
		t.Fatalf("token = %q, want empty", got)
	}
}
