package resourceservergithub

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/clems4ever/granular/resourceserver"
	"github.com/clems4ever/granular/resourceserver-github/internal/authz"
)

// DefaultGitHubAPIUpstream is the GitHub REST API origin the REST proxy forwards to.
const DefaultGitHubAPIUpstream = "https://api.github.com"

// restMountPrefix is the path subtree the REST proxy is mounted at; it is stripped before
// forwarding so /api/github/repos/o/r reaches the upstream as /repos/o/r.
const restMountPrefix = "/api/github"

// apiRoute refines one GitHub REST endpoint to a precise granular permission. It is pure
// data: a method and a slash path pattern whose {name} segments capture variables, plus the
// action, resource and whether the request body must be content-bound into the grant. This
// is the declarative alternative to a hand-written operation type — adding an endpoint is a
// table row, not a new file.
type apiRoute struct {
	method   string
	pattern  string
	action   string
	mutating bool
	bindBody bool // bind the grant to the exact request body (content hash in the requirement context)
	resource func(vars map[string]string) authz.ResourceRef
}

// apiRoutes is the refinement table: the endpoints granular gates at fine granularity. Any
// request that matches none of these still falls through to the coarse method+path default
// in requirementFor, so the whole API is gated even before a route is added here.
var apiRoutes = []apiRoute{
	{
		method: http.MethodPost, pattern: "/repos/{owner}/{repo}/pulls/{number}/reviews",
		action: "pull.review", mutating: true, bindBody: true,
		resource: func(v map[string]string) authz.ResourceRef {
			return authz.PullRef(v["owner"]+"/"+v["repo"], atoi(v["number"]))
		},
	},
	{
		method: http.MethodPost, pattern: "/repos/{owner}/{repo}/issues",
		action: "issue.create", mutating: true, bindBody: true,
		resource: func(v map[string]string) authz.ResourceRef {
			return authz.RepoRef(v["owner"] + "/" + v["repo"])
		},
	},
	{
		method: http.MethodGet, pattern: "/repos/{owner}/{repo}/pulls",
		action: "pull.list", mutating: false,
		resource: func(v map[string]string) authz.ResourceRef {
			return authz.RepoRef(v["owner"] + "/" + v["repo"])
		},
	},
}

// RESTProxy is an authorizing reverse proxy for the GitHub REST API. It derives the granular
// permission a request needs from its method and path — precisely for endpoints in the
// refinement table, coarsely (read vs write on the path's resource) for everything else —
// authorizes that against the AS with the caller's subject token, and only then forwards the
// request to GitHub with the server-held PAT injected. One engine covers the whole API; new
// endpoints are gated by data, not by reimplementing each operation.
type RESTProxy struct {
	authorizer Authorizer
	pat        string
	upstream   *url.URL
	routes     []apiRoute
	proxy      *httputil.ReverseProxy
}

// NewRESTProxy builds a RESTProxy forwarding to the GitHub REST API, injecting pat upstream
// and gating every request through a.
//
// @arg pat The server-held GitHub token injected upstream.
// @arg a The authorizer consulted before any request is forwarded.
// @return *RESTProxy A ready-to-mount handler for the /api/github/ subtree.
//
// @testcase TestRESTProxyForwardsWithPAT forwards an authorized call with the PAT.
func NewRESTProxy(pat string, a Authorizer) *RESTProxy {
	return newRESTProxy(pat, DefaultGitHubAPIUpstream, a)
}

// newRESTProxy builds a RESTProxy forwarding to an arbitrary upstream, so tests can point it
// at a fake GitHub API.
//
// @arg pat The server-held token injected upstream.
// @arg upstream The REST API origin to forward to.
// @arg a The authorizer consulted before forwarding.
// @return *RESTProxy The configured proxy.
//
// @testcase TestRESTProxyForwardsWithPAT builds a proxy against a fake upstream.
func newRESTProxy(pat, upstream string, a Authorizer) *RESTProxy {
	up, _ := url.Parse(upstream)
	g := &RESTProxy{authorizer: a, pat: pat, upstream: up, routes: apiRoutes}
	g.proxy = &httputil.ReverseProxy{Director: g.direct}
	return g
}

// ServeHTTP derives the permission the request needs, authorizes it against the AS, and on an
// allow forwards it to GitHub. A request with no credentials is challenged with 401; a denied
// request is 403; a bad body (when content-binding) is 400; an AS failure is 502.
//
// @arg w The response writer.
// @arg r The incoming GitHub REST API request under /api/github/.
//
// @testcase TestRESTProxyRefinedRouteMapsToAction maps a known endpoint to its action.
// @testcase TestRESTProxyDefaultMapping gates an unmapped endpoint by method and path.
// @testcase TestRESTProxyDeniesUnauthorized returns 403 when the AS denies.
// @testcase TestRESTProxyRequiresToken challenges an unauthenticated request.
func (g *RESTProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	apiPath := strings.TrimPrefix(r.URL.Path, restMountPrefix)
	reqmt, err := g.requirementFor(r, apiPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token := subjectToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="granular"`)
		http.Error(w, "missing subject token", http.StatusUnauthorized)
		return
	}
	allowed, err := g.authorizer.Authorize(r.Context(), token, []resourceserver.Requirement{reqmt})
	if err != nil {
		http.Error(w, "authorization check failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !allowed {
		http.Error(w, fmt.Sprintf("not authorized to %s %s", reqmt.Action, apiPath), http.StatusForbidden)
		return
	}
	g.proxy.ServeHTTP(w, r)
}

// requirementFor maps an incoming request to the single authorization requirement it needs.
// A refinement-table match yields the precise action, resource and (when bindBody) a hash of
// the exact request body so a grant authorizes only that body. Otherwise it falls back to a
// coarse default — github.read for GET/HEAD, github.write for any mutating method — on the
// resource named by the path, so an unmapped endpoint still fails closed.
//
// @arg r The incoming request; its body is read and restored when content-binding.
// @arg apiPath The request path with the mount prefix already stripped.
// @return resourceserver.Requirement The single check the request must pass.
// @error error when content-binding cannot read the request body.
//
// @testcase TestRESTProxyRefinedRouteMapsToAction builds a content-bound requirement.
// @testcase TestRESTProxyDefaultMapping builds the coarse read/write requirement.
func (g *RESTProxy) requirementFor(r *http.Request, apiPath string) (resourceserver.Requirement, error) {
	if rt, vars := g.match(r.Method, apiPath); rt != nil {
		req := resourceserver.Requirement{Action: rt.action, Resource: convertRef(rt.resource(vars))}
		if rt.bindBody {
			h, err := bodyHash(r)
			if err != nil {
				return resourceserver.Requirement{}, fmt.Errorf("read request body: %w", err)
			}
			req.Context = map[string]string{"body_sha256": h}
		}
		return req, nil
	}
	action := "github.write"
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		action = "github.read"
	}
	return resourceserver.Requirement{Action: action, Resource: convertRef(resourceFromPath(apiPath))}, nil
}

// match finds the first refinement route whose method and segment pattern fit the path,
// returning it together with the captured {name} variables.
//
// @arg method The request method.
// @arg apiPath The prefix-stripped request path.
// @return *apiRoute The matching route, or nil when none matches.
// @return map[string]string The captured path variables (nil when no match).
//
// @testcase TestRESTProxyRefinedRouteMapsToAction matches a reviews endpoint.
// @testcase TestRESTProxyDefaultMapping returns no match for an unmapped path.
func (g *RESTProxy) match(method, apiPath string) (*apiRoute, map[string]string) {
	segs := segments(apiPath)
	for i := range g.routes {
		rt := &g.routes[i]
		if rt.method != method {
			continue
		}
		psegs := segments(rt.pattern)
		if len(psegs) != len(segs) {
			continue
		}
		vars := map[string]string{}
		ok := true
		for j, ps := range psegs {
			switch {
			case strings.HasPrefix(ps, "{") && strings.HasSuffix(ps, "}"):
				vars[ps[1:len(ps)-1]] = segs[j]
			case ps != segs[j]:
				ok = false
			}
			if !ok {
				break
			}
		}
		if ok {
			return rt, vars
		}
	}
	return nil, nil
}

// direct rewrites an outbound request onto the GitHub REST API: it strips the mount prefix,
// points the request at the upstream host and replaces any client credentials with the
// server-held PAT as a Bearer token (or forwards anonymously when no PAT is configured, so a
// missing token never becomes an empty credential).
//
// @arg req The outbound request the ReverseProxy is about to send.
//
// @testcase TestRESTProxyForwardsWithPAT checks the rewritten path and the Bearer PAT.
func (g *RESTProxy) direct(req *http.Request) {
	req.URL.Scheme = g.upstream.Scheme
	req.URL.Host = g.upstream.Host
	req.Host = g.upstream.Host
	req.URL.Path = strings.TrimPrefix(req.URL.Path, restMountPrefix)
	if g.pat == "" {
		req.Header.Del("Authorization")
		return
	}
	req.Header.Set("Authorization", "Bearer "+g.pat)
}

// resourceFromPath names the resource a request acts on for the coarse default mapping:
// the repo for /repos/{owner}/{name}/..., the org for /orgs/{owner} or /users/{owner}, else
// the API as a whole.
//
// @arg apiPath The prefix-stripped request path.
// @return authz.ResourceRef The resource the coarse requirement is scoped to.
//
// @testcase TestRESTProxyDefaultMapping scopes the default requirement to the repo.
func resourceFromPath(apiPath string) authz.ResourceRef {
	segs := segments(apiPath)
	if len(segs) >= 3 && segs[0] == "repos" {
		return authz.RepoRef(segs[1] + "/" + segs[2])
	}
	if len(segs) >= 2 && (segs[0] == "orgs" || segs[0] == "users") {
		return authz.OrgRef(segs[1])
	}
	return authz.ResourceRef{Type: "github.api", ID: "github"}
}

// bodyHash reads the request body to compute its SHA-256 and restores it so the request can
// still be forwarded unchanged.
//
// @arg r The request whose body is hashed and restored.
// @return string The hex SHA-256 of the body ("" body hashes the empty input).
// @error error when the body cannot be read.
//
// @testcase TestRESTProxyRefinedRouteMapsToAction binds the body and still forwards it.
func bodyHash(r *http.Request) (string, error) {
	if r.Body == nil {
		return sha256hex(nil), nil
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	r.ContentLength = int64(len(b))
	return sha256hex(b), nil
}

// sha256hex returns the hex-encoded SHA-256 of b.
//
// @arg b The bytes to hash.
// @return string The hex digest.
//
// @testcase TestRESTProxyRefinedRouteMapsToAction relies on a stable body digest.
func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// segments splits a slash path into its non-empty parts.
//
// @arg p The path to split.
// @return []string The non-empty, slash-separated segments.
//
// @testcase TestRESTProxyDefaultMapping splits request paths into segments.
func segments(p string) []string {
	out := make([]string, 0, 6)
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// atoi parses a positive integer path segment, returning 0 when it is not a number.
//
// @arg s The segment to parse.
// @return int The parsed value, or 0 on failure.
//
// @testcase TestRESTProxyRefinedRouteMapsToAction parses the pull number from the path.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
