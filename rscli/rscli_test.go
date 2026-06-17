package rscli

import (
	"bytes"
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
	"github.com/spf13/cobra"
)

// stubRS is an in-memory resource server for tests. It serves a schema, signs
// grant requests, and echoes operation params back as the result. Fields capture
// the last request for assertions.
type stubRS struct {
	denyOps       bool
	lastOp        resourceserver.OperationRequest
	lastAuth      string
	signCalled    bool
	proposalAuth  string
	lastApprover  string
	proposalItems int
}

// schema returns a small representative schema the stub serves.
func (s *stubRS) schema() resourceserver.Schema {
	return resourceserver.Schema{
		Resources: []resourceserver.ResourceType{{
			Name: "github.repo", Title: "Repository",
			Match: []resourceserver.MatchField{{Name: "owner", Type: "string", Description: "repo owner"}},
		}},
		Actions:    []resourceserver.Action{{Name: "repo.clone", Title: "Clone", Resource: "github.repo"}},
		Operations: []resourceserver.OperationSpec{{Type: "github.clone", Action: "repo.clone", Resource: "github.repo", Params: []resourceserver.Param{{Name: "repo", Type: "string", Required: true}}}},
		Templates:  []resourceserver.Template{{Name: "repo-clone", Title: "Clone access", Actions: []string{"repo.clone"}, Scope: "github.repo"}},
	}
}

// newStubServer starts an httptest server backed by the stub.
func newStubServer(s *stubRS) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(s.schema())
	})
	mux.HandleFunc("/api/grant-requests/sign", func(w http.ResponseWriter, r *http.Request) {
		s.signCalled = true
		_ = json.NewEncoder(w).Encode(proposal.SignedGrantRequest{
			ResourceServerID: "github",
			Presentation:     proposal.Presentation{Title: "Access request", Summary: "Grant 1 permission set"},
			Policies:         []string{"permit (...);"},
			Signature:        "deadbeef",
		})
	})
	mux.HandleFunc("/api/proposals", func(w http.ResponseWriter, r *http.Request) {
		s.proposalAuth = r.Header.Get("Authorization")
		var in struct {
			ApproverEmail string            `json:"approver_email"`
			Items         []json.RawMessage `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		s.lastApprover, s.proposalItems = in.ApproverEmail, len(in.Items)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"proposal_id": "prop-1",
			"url":         "http://approve.example/prop-1",
			"expires_at":  "2026-06-18T00:00:00Z",
		})
	})
	mux.HandleFunc("/api/operations", func(w http.ResponseWriter, r *http.Request) {
		s.lastAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&s.lastOp)
		if s.denyOps {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(client.Result{Status: "denied"})
			return
		}
		_ = json.NewEncoder(w).Encode(client.Result{Status: "ok", Result: s.lastOp.Params})
	})
	return httptest.NewServer(mux)
}

// echoSpec builds a Spec with one typed operation command pointed at baseURL.
func echoSpec(baseURL string) Spec {
	return Spec{
		Use: "rs", RSID: "github", DefaultBaseURL: baseURL,
		Operations: []OpCommand{{
			Path: "echo", Type: "test.echo", Short: "echo params",
			Flags: []Flag{
				{Name: "name", Required: true},
				{Name: "count", Type: IntFlag},
				{Name: "loud", Type: BoolFlag},
				{Name: "tags", Type: StringSliceFlag},
			},
		}},
	}
}

// run executes the root command with args and returns its output and error. It
// neutralizes the default config and token files so tests never depend on the
// host's ~/.granular contents.
func run(spec Spec, args ...string) (string, error) {
	var buf bytes.Buffer
	root := NewRootCmd(spec, &buf)
	root.SetArgs(append([]string{"--config", "/dev/null", "--token-file", "/dev/null"}, args...))
	err := root.Execute()
	return buf.String(), err
}

// --- config ---

// TestLoadConfigMissingIsEmpty checks a missing config file yields an empty config.
func TestLoadConfigMissingIsEmpty(t *testing.T) {
	c, err := loadConfig(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.BaseURL != "" {
		t.Errorf("want empty config, got %+v", c)
	}
}

// TestLoadConfigReadsBaseURL checks the base URL and AS URL are parsed.
func TestLoadConfigReadsBaseURL(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), defaultBaseName("github"))
	if err := os.WriteFile(cfgPath, []byte("base_url: http://rs.example\nas_url: http://as.example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.BaseURL != "http://rs.example" {
		t.Errorf("base_url = %q", c.BaseURL)
	}
	if c.ASURL != "http://as.example" {
		t.Errorf("as_url = %q", c.ASURL)
	}
}

// defaultBaseName returns the base file name defaultConfigPath uses for rsID.
func defaultBaseName(rsID string) string { return rsID + ".yaml" }

// TestResolveTokenFlagOverrides checks --token wins and no file is read.
func TestResolveTokenFlagOverrides(t *testing.T) {
	got, err := resolveToken("flag-tok", "/no/such/file", true)
	if err != nil || got != "flag-tok" {
		t.Fatalf("resolveToken = (%q, %v), want flag-tok", got, err)
	}
}

// TestResolveTokenReadsFile checks the token is read and trimmed from the file.
func TestResolveTokenReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tok")
	if err := os.WriteFile(path, []byte("  secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveToken("", path, true)
	if err != nil || got != "secret" {
		t.Fatalf("resolveToken = (%q, %v), want secret", got, err)
	}
}

// TestResolveTokenMissingDefaultTolerated checks a missing default token file
// yields an empty token without error (so catalog/sign still work).
func TestResolveTokenMissingDefaultTolerated(t *testing.T) {
	got, err := resolveToken("", filepath.Join(t.TempDir(), "absent"), false)
	if err != nil || got != "" {
		t.Fatalf("resolveToken = (%q, %v), want empty", got, err)
	}
}

// TestResolveTokenExplicitMissingErrors checks a missing explicitly chosen token
// file is an error.
func TestResolveTokenExplicitMissingErrors(t *testing.T) {
	if _, err := resolveToken("", filepath.Join(t.TempDir(), "absent"), true); err == nil {
		t.Fatal("expected error for missing explicit token file")
	}
}

// TestLoadResolvesBaseURLFromConfig checks the config's base_url is used when no
// --base-url flag is given (so an agent need not pass the URL).
func TestLoadResolvesBaseURLFromConfig(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	cfgPath := filepath.Join(t.TempDir(), "github.yaml")
	if err := os.WriteFile(cfgPath, []byte("base_url: "+srv.URL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Use: "rs", RSID: "github", DefaultBaseURL: "http://wrong.invalid"}
	var buf bytes.Buffer
	root := NewRootCmd(spec, &buf)
	root.SetArgs([]string{"--config", cfgPath, "--token-file", "/dev/null", "catalog"})
	if err := root.Execute(); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if !strings.Contains(buf.String(), "github.repo") {
		t.Errorf("config base_url not used (stub not reached): %s", buf.String())
	}
}

// --- catalog ---

// TestCatalogPrintsSchema checks the human-readable catalog renders the schema.
func TestCatalogPrintsSchema(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	out, err := run(echoSpec(srv.URL), "catalog", "--base-url", srv.URL)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	for _, want := range []string{"github.repo", "Repository", "repo.clone", "github.clone", "repo-clone"} {
		if !strings.Contains(out, want) {
			t.Errorf("catalog output missing %q\n%s", want, out)
		}
	}
}

// TestCatalogJSON checks catalog --json emits decodable JSON.
func TestCatalogJSON(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	out, err := run(echoSpec(srv.URL), "catalog", "--json", "--base-url", srv.URL)
	if err != nil {
		t.Fatalf("catalog --json: %v", err)
	}
	var schema resourceserver.Schema
	if err := json.Unmarshal([]byte(out), &schema); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if len(schema.Operations) != 1 || schema.Operations[0].Type != "github.clone" {
		t.Errorf("decoded schema unexpected: %+v", schema.Operations)
	}
}

// --- sign ---

// TestSignWritesSignedRequest checks sign writes the signed request to the writer.
func TestSignWritesSignedRequest(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	out, err := run(echoSpec(srv.URL), "sign", "--base-url", srv.URL,
		"--actions", "repo.clone", "--resource", "github.repo", "--match", "owner=clems4ever")
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !s.signCalled {
		t.Error("resource server sign endpoint not called")
	}
	var sgr proposal.SignedGrantRequest
	if err := json.Unmarshal([]byte(out), &sgr); err != nil {
		t.Fatalf("sign output not JSON: %v\n%s", err, out)
	}
	if sgr.ResourceServerID != "github" || sgr.Signature != "deadbeef" {
		t.Errorf("unexpected signed request: %+v", sgr)
	}
}

// TestSignToFile checks sign --out writes the signed request to a file.
func TestSignToFile(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "signed.json")
	out, err := run(echoSpec(srv.URL), "sign", "--base-url", srv.URL, "--out", path,
		"--actions", "repo.clone", "--resource", "github.repo")
	if err != nil {
		t.Fatalf("sign --out: %v", err)
	}
	if !strings.Contains(out, path) {
		t.Errorf("expected confirmation mentioning %q, got %q", path, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading signed file: %v", err)
	}
	var sgr proposal.SignedGrantRequest
	if err := json.Unmarshal(data, &sgr); err != nil {
		t.Fatalf("file not JSON: %v", err)
	}
}

// TestRequestSubmitsAndPrintsURL checks `request` signs and submits a grant
// request to the AS and prints the approval URL.
func TestRequestSubmitsAndPrintsURL(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	out, err := run(echoSpec(srv.URL), "request", "--base-url", srv.URL, "--as-url", srv.URL,
		"--token", "subj-tok", "--approver", "alice@example.com",
		"--actions", "repo.clone", "--resource", "github.repo", "--match", "owner=clems4ever")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if !s.signCalled {
		t.Error("sign endpoint not called")
	}
	if s.lastApprover != "alice@example.com" || s.proposalItems != 1 {
		t.Errorf("proposal approver=%q items=%d", s.lastApprover, s.proposalItems)
	}
	if s.proposalAuth != "Bearer subj-tok" {
		t.Errorf("proposal auth = %q, want Bearer subj-tok", s.proposalAuth)
	}
	if !strings.Contains(out, "http://approve.example/prop-1") {
		t.Errorf("approval URL not printed: %q", out)
	}
}

// TestRequestNeedsASURL checks `request` errors clearly when no AS URL is set.
func TestRequestNeedsASURL(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	_, err := run(echoSpec(srv.URL), "request", "--base-url", srv.URL, "--token", "t",
		"--approver", "a@b.com", "--actions", "repo.clone", "--resource", "github.repo")
	if err == nil || !strings.Contains(err.Error(), "authorization server URL") {
		t.Fatalf("err = %v, want a clear no-AS-URL error", err)
	}
}

// TestBuildSignRequestTemplate checks template form assembly and binding parsing.
func TestBuildSignRequestTemplate(t *testing.T) {
	req, err := buildSignRequest("repo-clone", []string{"owner=clems4ever"}, "", nil, "", "")
	if err != nil {
		t.Fatalf("buildSignRequest: %v", err)
	}
	if req.Template != "repo-clone" || req.Bindings["owner"] != "clems4ever" {
		t.Errorf("unexpected template request: %+v", req)
	}
	if _, err := buildSignRequest("repo-clone", []string{"bad"}, "", nil, "", ""); err == nil {
		t.Error("expected error on malformed binding")
	}
}

// TestBuildSignRequestFreeform checks freeform capability assembly and match parsing.
func TestBuildSignRequestFreeform(t *testing.T) {
	req, err := buildSignRequest("", nil, "because", []string{"repo.clone"}, "github.repo", "owner=clems4ever,name=granular")
	if err != nil {
		t.Fatalf("buildSignRequest: %v", err)
	}
	if len(req.Capabilities) != 1 {
		t.Fatalf("want 1 capability, got %d", len(req.Capabilities))
	}
	cp := req.Capabilities[0]
	if cp.Resource.Type != "github.repo" || cp.Resource.Match["name"] != "granular" || cp.Resource.Match["owner"] != "clems4ever" {
		t.Errorf("unexpected capability: %+v", cp)
	}
}

// --- operations ---

// TestOperationCommandSendsTypedParams checks typed flags map to native params
// and unset optional flags are omitted (preserving resource-server defaults).
func TestOperationCommandSendsTypedParams(t *testing.T) {
	s := &stubRS{}
	srv := newStubServer(s)
	defer srv.Close()

	out, err := run(echoSpec(srv.URL), "echo", "--base-url", srv.URL, "--token", "t",
		"--name", "widget", "--count", "3", "--tags", "a", "--tags", "b")
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	p := s.lastOp.Params
	if p["name"] != "widget" {
		t.Errorf("name = %v, want widget", p["name"])
	}
	if p["count"] != float64(3) { // JSON number decodes to float64
		t.Errorf("count = %#v, want 3", p["count"])
	}
	tags, ok := p["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %#v, want [a b]", p["tags"])
	}
	if _, present := p["loud"]; present {
		t.Errorf("unset optional bool 'loud' should be omitted, got %#v", p["loud"])
	}
	// The result is echoed back and printed.
	if !strings.Contains(out, "widget") {
		t.Errorf("result not printed: %s", out)
	}
}

// TestOperationCommandDenialMessage checks a policy denial prints guidance and errors.
func TestOperationCommandDenialMessage(t *testing.T) {
	s := &stubRS{denyOps: true}
	srv := newStubServer(s)
	defer srv.Close()

	out, err := run(echoSpec(srv.URL), "echo", "--base-url", srv.URL, "--token", "t", "--name", "x")
	if err != client.ErrNotAuthorized {
		t.Fatalf("err = %v, want ErrNotAuthorized", err)
	}
	if !strings.Contains(out, "Not authorized") {
		t.Errorf("missing guidance: %q", out)
	}
}

// --- wiring ---

// TestNewRootCmdWiresBuiltins checks the root carries catalog, sign, and the
// declared operation commands (with multi-word paths nested under a group).
func TestNewRootCmdWiresBuiltins(t *testing.T) {
	spec := Spec{
		Use: "rs", RSID: "github",
		Operations: []OpCommand{{Path: "issue create", Type: "github.issue.create", Flags: []Flag{{Name: "repo", Required: true}}}},
	}
	root := NewRootCmd(spec, &bytes.Buffer{})

	if findCmd(root, "catalog") == nil || findCmd(root, "sign") == nil {
		t.Fatal("built-in catalog/sign commands missing")
	}
	issue := findCmd(root, "issue")
	if issue == nil {
		t.Fatal("issue group command missing")
	}
	if findCmd(issue, "create") == nil {
		t.Error("issue create command missing")
	}
}

// findCmd returns the immediate subcommand of parent named name, or nil.
func findCmd(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
