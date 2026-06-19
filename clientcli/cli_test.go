package clientcli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clems4ever/granular/client"
	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/resourceserver"
)

// fakeResourceServer is a minimal resource server server for the CLI tests.
//
// @arg t The test handle.
// @return *httptest.Server The running fake resource server.
//
// @testcase TestRunCatalog catalogs this resource server.
func fakeResourceServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(resourceserver.Schema{
			Resources: []resourceserver.ResourceType{
				{Name: "t.org", Title: "Org"},
				{Name: "t.repo", Title: "Repo", Parent: "t.org",
					Match: []resourceserver.MatchField{{Name: "owner", Type: "string", Description: "owner login"}}},
			},
			Groups:  []resourceserver.Group{{Name: "read", Description: "Everything readable."}},
			Actions: []resourceserver.Action{{Name: "repo.read", Title: "Read repo", Resource: "t.repo", Groups: []string{"read"}}},
			Operations: []resourceserver.OperationSpec{{
				Type: "t.clone", Title: "Clone", Action: "repo.read", Resource: "t.repo",
				Params: []resourceserver.Param{{Name: "repo", Type: "string", Required: true, Description: "owner/name"}},
			}},
			Templates: []resourceserver.Template{{
				Name: "read-repo", Title: "Read a repo", Scope: "t.repo", Actions: []string{"read"},
				Summary:     "Read {owner}",
				Description: "Read access.",
				Params: []resourceserver.TemplateParam{
					{Name: "owner", Field: "owner", Required: true, Description: "owner login"},
					{Name: "label", Attr: "labels", Op: "contains", Description: "only labeled"},
				},
			}},
			Example: resourceserver.GrantRequest{Capabilities: []resourceserver.Capability{{
				Actions:  []string{"repo.read"},
				Resource: resourceserver.ResourceSelector{Type: "t.repo", Match: map[string]string{"owner": "o"}},
			}}},
		})
	})
	mux.HandleFunc("POST /api/grant-requests/sign", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(proposal.Sign([]byte("s"), "g1", proposal.Presentation{Title: "t"}, []string{"permit;"}))
	})
	mux.HandleFunc("POST /api/operations", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Result{Status: "completed", Result: map[string]any{"ok": true}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// fakeAS is a minimal AS server exposing the proposals endpoint for the CLI tests.
//
// @arg t The test handle.
// @return *httptest.Server The running fake AS.
//
// @testcase TestRunPropose drives this AS.
func fakeAS(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/proposals", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"proposal_id": "p1", "url": "http://as/proposal/p1"})
	})
	mux.HandleFunc("GET /api/subject/me", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": []client.Grant{{ResourceServerID: "g1", ExpiresAt: "soon"}}})
	})
	mux.HandleFunc("DELETE /api/subject/me/grants", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"revoked": 3})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// newClient builds an SDK client over the given resource server and AS URLs with a token.
//
// @arg asURL The AS base URL.
// @arg rsURL The resource server base URL.
// @return *client.Client A configured client.
//
// @testcase TestRunOp builds a client for the op test.
func newClient(asURL, rsURL string) *client.Client {
	return client.New(client.Config{ASURL: asURL, Token: "tok", ResourceServers: []client.ResourceServer{{ID: "g1", BaseURL: rsURL}}})
}

// TestCommandTree checks the sub-commands are wired under the root.
func TestCommandTree(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{})
	want := map[string]bool{"catalog": true, "template": true, "op": true, "sign": true, "propose": true, "grants": true, "revoke": true}
	for _, c := range root.Commands() {
		delete(want, c.Name())
	}
	if len(want) != 0 {
		t.Fatalf("missing commands: %v", want)
	}
}

// TestRunGrants lists the grants attached to the caller's own subject token.
func TestRunGrants(t *testing.T) {
	as := fakeAS(t)
	c := newClient(as.URL, "")
	var buf bytes.Buffer
	if err := runGrants(context.Background(), c, &buf); err != nil {
		t.Fatalf("grants: %v", err)
	}
	if !strings.Contains(buf.String(), "g1") {
		t.Fatalf("grants output missing g1: %q", buf.String())
	}
}

// TestRunRevokeGrants revokes all grants on the caller's own subject token and reports the count.
func TestRunRevokeGrants(t *testing.T) {
	as := fakeAS(t)
	c := newClient(as.URL, "")
	var buf bytes.Buffer
	if err := runRevokeGrants(context.Background(), c, &buf); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !strings.Contains(buf.String(), "revoked 3 grants") {
		t.Fatalf("revoke output unexpected: %q", buf.String())
	}
}

// TestDefaultConfig checks the built-in defaults.
func TestDefaultConfig(t *testing.T) {
	if Default().ASURL != "http://localhost:9090" {
		t.Fatalf("unexpected default AS url")
	}
}

// TestLoadParsesConfig loads resource servers and resolves the token file.
func TestLoadParsesConfig(t *testing.T) {
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "tok")
	if err := os.WriteFile(tokFile, []byte("  secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "client.yaml")
	body := "as_url: http://as:9090\ntoken_file: " + tokFile + "\nresource_servers:\n  - id: g1\n    base_url: http://rs\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.ASURL != "http://as:9090" || c.Token != "secret" || len(c.ResourceServers) != 1 || c.ResourceServers[0].ID != "g1" {
		t.Fatalf("unexpected config: %+v", c)
	}
}

// TestLoadMissingTokenFile errors when the configured token file is absent.
func TestLoadMissingTokenFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "client.yaml")
	body := "token_file: " + filepath.Join(dir, "absent") + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("expected error for a missing token file")
	}
}

// TestToClientUsesOverride prefers the override token over the configured one.
func TestToClientUsesOverride(t *testing.T) {
	cfg := &Config{ASURL: "http://as", Token: "configured", ResourceServers: []resourceServerConf{{ID: "g1", BaseURL: "http://rs"}}}
	c := cfg.toClient("override")
	if c.Token() != "override" {
		t.Fatalf("token = %q, want override", c.Token())
	}
	if c2 := cfg.toClient(""); c2.Token() != "configured" {
		t.Fatalf("token = %q, want configured", c2.Token())
	}
}

// TestParseParams parses pairs and rejects a malformed entry.
func TestParseParams(t *testing.T) {
	p, err := parseParams([]string{"repo=octo/hello", "n=1"})
	if err != nil || p["repo"] != "octo/hello" || p["n"] != "1" {
		t.Fatalf("params = %v, err = %v", p, err)
	}
	if _, err := parseParams([]string{"bad"}); err == nil {
		t.Fatal("expected error for malformed pair")
	}
}

// TestParseMatch parses multiple fields and the empty string.
func TestParseMatch(t *testing.T) {
	m := parseMatch("owner=octo, name=hello")
	if m["owner"] != "octo" || m["name"] != "hello" {
		t.Fatalf("match = %v", m)
	}
	if len(parseMatch("")) != 0 {
		t.Fatal("empty match should be empty")
	}
}

// TestBuildGrantRequest builds a one-capability grant request from flags.
func TestBuildGrantRequest(t *testing.T) {
	req := buildGrantRequest("work", []string{"repo.read"}, "t.repo", map[string]string{"owner": "o"})
	if req.Reason != "work" || len(req.Capabilities) != 1 {
		t.Fatalf("request = %+v", req)
	}
	cap := req.Capabilities[0]
	if cap.Actions[0] != "repo.read" || cap.Resource.Type != "t.repo" || cap.Resource.Match["owner"] != "o" {
		t.Fatalf("capability = %+v", cap)
	}
}

// TestBuildSignRequest builds both the template and freeform forms and rejects a bad bind.
func TestBuildSignRequest(t *testing.T) {
	tpl, err := buildSignRequest("comment", []string{"owner=octo", "name=hello"}, "", nil, "", "")
	if err != nil || tpl.Template != "comment" || tpl.Bindings["owner"] != "octo" || tpl.Bindings["name"] != "hello" {
		t.Fatalf("template form = %+v err=%v", tpl, err)
	}
	free, err := buildSignRequest("", nil, "work", []string{"repo.read"}, "t.repo", "owner=o")
	if err != nil || free.Template != "" || len(free.Capabilities) != 1 || free.Capabilities[0].Resource.Match["owner"] != "o" {
		t.Fatalf("freeform form = %+v err=%v", free, err)
	}
	if _, err := buildSignRequest("comment", []string{"bad"}, "", nil, "", ""); err == nil {
		t.Fatal("expected error for malformed bind")
	}
}

// TestRunCatalog prints resources (with match fields), actions and the example so an
// agent has everything it needs to build a grant request.
func TestRunCatalog(t *testing.T) {
	rs := fakeResourceServer(t)
	c := newClient("http://as", rs.URL)
	var buf bytes.Buffer
	if err := runCatalog(context.Background(), c, nil, false, &buf); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"t.repo", "owner", // resource + match field
		"→ repo.read",                            // group expansion
		"needs action repo.read on t.repo",       // operation + required action/resource
		"*repo",                                  // required param marker
		"read-repo",                              // template name
		"--actions repo.read", "--match owner=o", // example
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("catalog output missing %q:\n%s", want, out)
		}
	}
}

// TestRunTemplate lists templates and details one by name, showing its expanded actions,
// scope and conditions.
func TestRunTemplate(t *testing.T) {
	rs := fakeResourceServer(t)
	c := newClient("http://as", rs.URL)

	var list bytes.Buffer
	if err := runTemplate(context.Background(), c, nil, "", &list); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(list.String(), "read-repo") || !strings.Contains(list.String(), "grants read on a t.repo") {
		t.Fatalf("list output = %q", list.String())
	}

	var detail bytes.Buffer
	if err := runTemplate(context.Background(), c, nil, "read-repo", &detail); err != nil {
		t.Fatalf("detail: %v", err)
	}
	for _, want := range []string{
		"read-repo",               // header
		"i.e. repo.read",          // group "read" expanded to concrete action
		"a t.repo",                // scope
		"labels contains <label>", // parameterized condition
		"condition (labels)",      // param role
		"--bind owner=",           // sign hint
	} {
		if !strings.Contains(detail.String(), want) {
			t.Fatalf("detail missing %q:\n%s", want, detail.String())
		}
	}

	if err := runTemplate(context.Background(), c, nil, "nope", &bytes.Buffer{}); err == nil {
		t.Fatal("expected error for unknown template")
	}
}

// TestRunCatalogJSON prints the raw schema as JSON for programmatic consumption.
func TestRunCatalogJSON(t *testing.T) {
	rs := fakeResourceServer(t)
	c := newClient("http://as", rs.URL)
	var buf bytes.Buffer
	if err := runCatalog(context.Background(), c, nil, true, &buf); err != nil {
		t.Fatalf("catalog --json: %v", err)
	}
	var got map[string]resourceserver.Schema
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	s, ok := got["g1"]
	if !ok || len(s.Resources) != 2 || !s.HasAction("repo.read") || len(s.Operations) != 1 || s.Operations[0].Params[0].Name != "repo" {
		t.Fatalf("unexpected schema json: %+v", got)
	}
}

// TestRunOp prints the result of an authorized operation.
func TestRunOp(t *testing.T) {
	rs := fakeResourceServer(t)
	c := newClient("http://as", rs.URL)
	var buf bytes.Buffer
	if err := runOp(context.Background(), c, "g1", "github.clone", map[string]any{"repo": "o/r"}, &buf); err != nil {
		t.Fatalf("op: %v", err)
	}
	if !strings.Contains(buf.String(), "completed") {
		t.Fatalf("output = %q", buf.String())
	}
}

// TestRunSign signs a request and writes it to a file, then re-reads it as valid.
func TestRunSign(t *testing.T) {
	rs := fakeResourceServer(t)
	c := newClient("http://as", rs.URL)
	out := filepath.Join(t.TempDir(), "req.json")
	req := buildGrantRequest("work", []string{"repo.read"}, "t.repo", map[string]string{"owner": "o"})
	var buf bytes.Buffer
	if err := runSign(context.Background(), c, "g1", req, out, &buf); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.Contains(buf.String(), out) {
		t.Fatalf("output = %q", buf.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var sgr proposal.SignedGrantRequest
	if err := json.Unmarshal(data, &sgr); err != nil || sgr.ResourceServerID != "g1" {
		t.Fatalf("stored request invalid: %v %+v", err, sgr)
	}
}

// TestRunPropose bundles signed requests from files and prints the approval URL.
func TestRunPropose(t *testing.T) {
	rs, as := fakeResourceServer(t), fakeAS(t)
	c := newClient(as.URL, rs.URL)

	// Produce a stored signed request via sign.
	f := filepath.Join(t.TempDir(), "req.json")
	req := buildGrantRequest("work", []string{"repo.read"}, "t.repo", map[string]string{"owner": "o"})
	if err := runSign(context.Background(), c, "g1", req, f, &bytes.Buffer{}); err != nil {
		t.Fatalf("sign: %v", err)
	}

	var buf bytes.Buffer
	if err := runPropose(context.Background(), c, "a@b.c", "needs to read the repo", []string{f}, &buf); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if !strings.Contains(buf.String(), "/proposal/p1") {
		t.Fatalf("output = %q", buf.String())
	}
}
