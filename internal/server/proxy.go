package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/clems4ever/granular/internal/authz"
	githubops "github.com/clems4ever/granular/internal/operations/github"
)

// githubHost is the upstream the git proxy forwards to.
var githubHost = &url.URL{Scheme: "https", Host: "github.com"}

// handleGitProxy reverse-proxies git smart-HTTP requests under /git/ to
// github.com, injecting the server-held PAT, after verifying a live grant exists
// for the targeted repository. Read (upload-pack / clone) traffic requires a
// repo.clone grant; write (receive-pack / push) traffic requires a repo.push
// grant, so cloning and pushing are authorised independently.
//
// @arg w The response writer; the upstream response is streamed through it.
// @arg r The git client's request carrying the {rest...} path value.
//
// @testcase TestGitProxyDeniesWithoutGrant returns 403 when no clone grant exists.
// @testcase TestGitProxyDeniesPushWithoutGrant returns 403 for receive-pack without a push grant.
func (s *Server) handleGitProxy(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest")
	repo, ok := repoFromGitPath(rest)
	if !ok {
		http.Error(w, "malformed git path", http.StatusNotFound)
		return
	}

	action, verb := "repo.clone", "clone"
	if isPush(rest, r) {
		action, verb = "repo.push", "push to"
	}

	allowed, err := s.authorize([]authz.Requirement{{Action: action, Resource: authz.RepoRef(repo)}})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "no live grant to "+verb+" "+repo+"; request approval first", http.StatusForbidden)
		return
	}

	proxy := &httputil.ReverseProxy{Director: func(req *http.Request) {
		req.URL.Scheme = githubHost.Scheme
		req.URL.Host = githubHost.Host
		req.URL.Path = "/" + rest
		req.URL.RawPath = ""
		req.Host = githubHost.Host
		if s.env.GitHubToken != "" {
			req.SetBasicAuth("granular", s.env.GitHubToken)
		}
	}}
	proxy.ServeHTTP(w, r)
}

// repoFromGitPath extracts the normalized "owner/name" repository from a git
// smart-HTTP path such as "owner/name.git/info/refs".
//
// @arg rest The path under /git/, without the leading slash or query string.
// @return string The normalized "owner/name" repository.
// @return bool True when the path had at least an owner and repo segment.
//
// @testcase TestRepoFromGitPath parses owner/name from a smart-HTTP path.
func repoFromGitPath(rest string) (string, bool) {
	parts := strings.SplitN(strings.TrimPrefix(rest, "/"), "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	name := strings.TrimSuffix(parts[1], ".git")
	if name == "" {
		return "", false
	}
	return githubops.NormalizeRepo(parts[0] + "/" + name), true
}

// isPush reports whether a git smart-HTTP request is a write (receive-pack), which
// the proxy refuses.
//
// @arg rest The path under /git/.
// @arg r The incoming request, whose query may name the git service.
// @return bool True when the request targets git-receive-pack.
//
// @testcase TestGitProxyRejectsPush exercises the receive-pack path.
func isPush(rest string, r *http.Request) bool {
	return strings.Contains(rest, "git-receive-pack") || r.URL.Query().Get("service") == "git-receive-pack"
}
