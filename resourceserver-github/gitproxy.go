package resourceservergithub

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/clems4ever/granular/resourceserver"
	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
)

// DefaultGitHubUpstream is the GitHub git-over-HTTPS origin the proxy forwards to.
const DefaultGitHubUpstream = "https://github.com"

// ctxKey is the private type for this package's request-context keys.
type ctxKey int

// anonymousKey marks a forwarded request that must reach the upstream with no
// credentials at all (a public-repo read), so direct injects neither the PAT nor the
// client's token.
const anonymousKey ctxKey = iota

// Authorizer decides whether a subject token is permitted a set of requirements.
// *resourceserver.ResourceServer satisfies it, so the git proxy gates a clone or push
// with exactly the same AS allow/deny decision the operations endpoint uses.
type Authorizer interface {
	Authorize(ctx context.Context, token string, reqs []resourceserver.Requirement) (bool, error)
}

// GitProxy is an authorizing git-smart-HTTP reverse proxy. It accepts a client's
// `git clone`/`git push` over HTTP at /git/<owner>/<repo>.git/..., reads the subject
// token from the request's HTTP basic-auth credentials, authorizes it against the AS for
// the repository (upload-pack needs repo.clone, receive-pack needs repo.push), and only
// then forwards the request to GitHub with the server-held PAT injected. The client never
// sees the PAT and the server never decides approval — it only enforces a prior grant.
//
// A read of a publicly cloneable repository is the exception: since anyone can already
// clone it anonymously, the proxy forwards it without a grant and without the PAT, so a
// public repo is never blocked for lack of an approval.
type GitProxy struct {
	authorizer Authorizer
	pat        string
	upstream   *url.URL
	proxy      *httputil.ReverseProxy
	client     *http.Client // probes the upstream to decide whether a repo is public
}

// NewGitProxy builds a GitProxy forwarding to GitHub (DefaultGitHubUpstream), injecting
// pat on the upstream request and gating every request through a.
//
// @arg pat The server-held GitHub personal access token injected upstream.
// @arg a The authorizer consulted before any request is forwarded.
// @return *GitProxy A ready-to-mount http.Handler for the /git/ subtree.
//
// @testcase TestGitProxyForwardsWithPAT forwards an authorized clone with the PAT.
func NewGitProxy(pat string, a Authorizer) *GitProxy {
	return newGitProxy(pat, DefaultGitHubUpstream, a)
}

// newGitProxy builds a GitProxy forwarding to an arbitrary upstream, so tests can point it
// at a fake GitHub.
//
// @arg pat The server-held token injected upstream.
// @arg upstream The git origin to forward to (e.g. https://github.com).
// @arg a The authorizer consulted before forwarding.
// @return *GitProxy The configured proxy.
//
// @testcase TestGitProxyForwardsWithPAT builds a proxy against a fake upstream.
func newGitProxy(pat, upstream string, a Authorizer) *GitProxy {
	up, _ := url.Parse(upstream)
	g := &GitProxy{
		authorizer: a,
		pat:        pat,
		upstream:   up,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
	g.proxy = &httputil.ReverseProxy{Director: g.direct}
	return g
}

// ServeHTTP authorizes the git request and, on an allow, forwards it to GitHub. A read
// (clone/fetch) of a publicly cloneable repository is served anonymously with no grant,
// since anyone could already clone it without credentials. Otherwise a request with no
// credentials is challenged with 401 so the client's git supplies them; a denied request
// is 403; a malformed path is 400; an AS failure is 502.
//
// @arg w The response writer.
// @arg r The incoming git smart-HTTP request.
//
// @testcase TestGitProxyRequiresToken challenges an unauthenticated request.
// @testcase TestGitProxyDeniesUnauthorized returns 403 when the AS denies.
// @testcase TestGitProxyForwardsWithPAT forwards an allowed request to the upstream.
// @testcase TestGitProxyServesPublicRepoWithoutGrant forwards a public-repo read with no grant.
// @testcase TestGitProxyRejectsBadPath rejects a non-git path.
func (g *GitProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo, service, err := parseGitRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	action, ok := serviceAction(service)
	if !ok {
		http.Error(w, "unsupported git service: "+service, http.StatusBadRequest)
		return
	}
	// A read of a public repo needs no grant: forward it anonymously, never touching the
	// PAT or the AS. Pushes (and reads of private repos) still go through authorization.
	if action == "repo.clone" && g.publiclyCloneable(r.Context(), repo) {
		g.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), anonymousKey, true)))
		return
	}
	token := subjectToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="granular"`)
		http.Error(w, "missing subject token", http.StatusUnauthorized)
		return
	}
	reqs := []resourceserver.Requirement{{Action: action, Resource: convertRef(authz.RepoRef(repo))}}
	allowed, err := g.authorizer.Authorize(r.Context(), token, reqs)
	if err != nil {
		http.Error(w, "authorization check failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !allowed {
		http.Error(w, fmt.Sprintf("not authorized to %s %s", action, repo), http.StatusForbidden)
		return
	}
	g.proxy.ServeHTTP(w, r)
}

// publiclyCloneable reports whether repo can be cloned from the upstream with no
// credentials at all, by probing its upload-pack ref advertisement anonymously. A 200
// means anyone can already read it, so the proxy may serve it without a grant. Any other
// status, or a probe error, is treated as not-public so the request falls through to
// authorization (fail closed).
//
// @arg ctx Context for cancellation, carried from the inbound request.
// @arg repo The "owner/repo" to probe.
// @return bool True when the upstream advertises the repo's refs without credentials.
//
// @testcase TestGitProxyServesPublicRepoWithoutGrant treats a 200 probe as public.
// @testcase TestGitProxyPrivateRepoStillAuthorizes treats a non-200 probe as private.
func (g *GitProxy) publiclyCloneable(ctx context.Context, repo string) bool {
	probeURL := g.upstream.String() + "/" + repo + ".git/info/refs?service=git-upload-pack"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// direct rewrites an outbound request onto the GitHub upstream: it strips the /git/ mount
// prefix, points the request at the upstream host, and replaces any client credentials
// with HTTP basic auth carrying the server-held PAT. A request marked anonymous (a public
// repo read) and one made when no PAT is configured are both forwarded with no credentials
// at all: the client's subject token is dropped so it never leaks upstream, and no empty
// "x-access-token:" credential is sent — GitHub rejects an invalid credential with 401 even
// for a public repo, which would otherwise clone fine without auth.
//
// @arg req The outbound request the ReverseProxy is about to send.
//
// @testcase TestGitProxyForwardsWithPAT checks the rewritten path and injected PAT.
// @testcase TestGitProxyForwardsAnonymouslyWithoutPAT forwards with no auth when the PAT is empty.
// @testcase TestGitProxyServesPublicRepoWithoutGrant forwards a public read with no credentials.
func (g *GitProxy) direct(req *http.Request) {
	req.URL.Scheme = g.upstream.Scheme
	req.URL.Host = g.upstream.Host
	req.Host = g.upstream.Host
	req.URL.Path = "/" + strings.TrimPrefix(req.URL.Path, "/git/")
	anon, _ := req.Context().Value(anonymousKey).(bool)
	if anon || g.pat == "" {
		req.Header.Del("Authorization")
		return
	}
	req.Header.Set("Authorization", "Basic "+basicAuth("x-access-token", g.pat))
}

// parseGitRequest extracts the owner/repo and the git service from a /git/ request. The
// service is the ?service= query for an info/refs ref-advertisement, or is named by the
// path for the git-upload-pack / git-receive-pack RPC endpoints.
//
// @arg r The incoming request whose path is /git/<owner>/<repo>.git/<git-path>.
// @return string The "owner/repo" the request targets.
// @return string The git service (git-upload-pack or git-receive-pack).
// @error error when the path is not a recognised git smart-HTTP path.
//
// @testcase TestParseGitRequest parses info/refs and the RPC endpoints.
// @testcase TestGitProxyRejectsBadPath rejects a path with no .git/ segment.
func parseGitRequest(r *http.Request) (string, string, error) {
	rest := strings.TrimPrefix(r.URL.Path, "/git/")
	i := strings.Index(rest, ".git/")
	if i < 0 {
		return "", "", fmt.Errorf("not a git smart-HTTP path: %s", r.URL.Path)
	}
	repo := rest[:i]
	if strings.Count(repo, "/") != 1 || strings.HasPrefix(repo, "/") || strings.HasSuffix(repo, "/") {
		return "", "", fmt.Errorf("expected owner/repo, got %q", repo)
	}
	switch gitPath := rest[i+len(".git/"):]; gitPath {
	case "info/refs":
		return repo, r.URL.Query().Get("service"), nil
	case "git-upload-pack":
		return repo, "git-upload-pack", nil
	case "git-receive-pack":
		return repo, "git-receive-pack", nil
	default:
		return "", "", fmt.Errorf("unsupported git path: %s", gitPath)
	}
}

// serviceAction maps a git service to the granular action it requires: fetching
// (git-upload-pack) needs repo.clone, pushing (git-receive-pack) needs repo.push.
//
// @arg service The git service name.
// @return string The required action.
// @return bool Whether the service is recognised.
//
// @testcase TestServiceAction maps both services and rejects an unknown one.
func serviceAction(service string) (string, bool) {
	switch service {
	case "git-upload-pack":
		return "repo.clone", true
	case "git-receive-pack":
		return "repo.push", true
	default:
		return "", false
	}
}

// subjectToken reads the subject token from a request's HTTP basic-auth credentials,
// preferring the password (git's `http://user:token@host` form) and falling back to the
// username when the password is empty.
//
// @arg r The incoming request.
// @return string The subject token, or "" when no credentials are present.
//
// @testcase TestGitProxyRequiresToken sees an empty token for an unauthenticated request.
// @testcase TestSubjectTokenFromBasicAuth reads the token from the password and username.
func subjectToken(r *http.Request) string {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return ""
	}
	if pass != "" {
		return pass
	}
	return user
}

// basicAuth encodes user and pass as an HTTP Basic credential (base64 of "user:pass").
//
// @arg user The username.
// @arg pass The password.
// @return string The base64-encoded "user:pass".
//
// @testcase TestGitProxyForwardsWithPAT observes the encoded upstream credential.
func basicAuth(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}
