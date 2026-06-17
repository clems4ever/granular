package main

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
	"github.com/clems4ever/granular/gateway"
	"github.com/clems4ever/granular/internal/proposal"
)

// fakeGateway is a minimal gateway server for the CLI tests.
//
// @arg t The test handle.
// @return *httptest.Server The running fake gateway.
//
// @testcase TestRunCatalog catalogs this gateway.
func fakeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gateway.Schema{Actions: []gateway.Action{{Name: "repo.read", Title: "Read repo"}}})
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
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// newClient builds an SDK client over the given gateway and AS URLs with a token.
//
// @arg asURL The AS base URL.
// @arg gwURL The gateway base URL.
// @return *client.Client A configured client.
//
// @testcase TestRunOp builds a client for the op test.
func newClient(asURL, gwURL string) *client.Client {
	return client.New(client.Config{ASURL: asURL, Token: "tok", Gateways: []client.Gateway{{ID: "g1", BaseURL: gwURL}}})
}

// TestMainIsEntryPoint is a placeholder: main only builds and executes the tree.
func TestMainIsEntryPoint(t *testing.T) { _ = main }

// TestCommandTree checks the sub-commands are wired under the root.
func TestCommandTree(t *testing.T) {
	root := newRootCmd(&bytes.Buffer{})
	want := map[string]bool{"catalog": true, "op": true, "sign": true, "propose": true}
	for _, c := range root.Commands() {
		delete(want, c.Name())
	}
	if len(want) != 0 {
		t.Fatalf("missing commands: %v", want)
	}
}

// TestDefaultConfig checks the built-in defaults.
func TestDefaultConfig(t *testing.T) {
	if Default().ASURL != "http://localhost:9090" {
		t.Fatalf("unexpected default AS url")
	}
}

// TestLoadParsesConfig loads gateways and resolves the token file.
func TestLoadParsesConfig(t *testing.T) {
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "tok")
	if err := os.WriteFile(tokFile, []byte("  secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "client.yaml")
	body := "as_url: http://as:9090\ntoken_file: " + tokFile + "\ngateways:\n  - id: g1\n    base_url: http://gw\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.ASURL != "http://as:9090" || c.Token != "secret" || len(c.Gateways) != 1 || c.Gateways[0].ID != "g1" {
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
	cfg := &Config{ASURL: "http://as", Token: "configured", Gateways: []gatewayConf{{ID: "g1", BaseURL: "http://gw"}}}
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

// TestRunCatalog prints a gateway's actions.
func TestRunCatalog(t *testing.T) {
	gw := fakeGateway(t)
	c := newClient("http://as", gw.URL)
	var buf bytes.Buffer
	if err := runCatalog(context.Background(), c, nil, &buf); err != nil {
		t.Fatalf("catalog: %v", err)
	}
	if !strings.Contains(buf.String(), "repo.read") {
		t.Fatalf("output = %q", buf.String())
	}
}

// TestRunOp prints the result of an authorized operation.
func TestRunOp(t *testing.T) {
	gw := fakeGateway(t)
	c := newClient("http://as", gw.URL)
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
	gw := fakeGateway(t)
	c := newClient("http://as", gw.URL)
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
	if err := json.Unmarshal(data, &sgr); err != nil || sgr.GatewayID != "g1" {
		t.Fatalf("stored request invalid: %v %+v", err, sgr)
	}
}

// TestRunPropose bundles signed requests from files and prints the approval URL.
func TestRunPropose(t *testing.T) {
	gw, as := fakeGateway(t), fakeAS(t)
	c := newClient(as.URL, gw.URL)

	// Produce a stored signed request via sign.
	f := filepath.Join(t.TempDir(), "req.json")
	req := buildGrantRequest("work", []string{"repo.read"}, "t.repo", map[string]string{"owner": "o"})
	if err := runSign(context.Background(), c, "g1", req, f, &bytes.Buffer{}); err != nil {
		t.Fatalf("sign: %v", err)
	}

	var buf bytes.Buffer
	if err := runPropose(context.Background(), c, "a@b.c", []string{f}, &buf); err != nil {
		t.Fatalf("propose: %v", err)
	}
	if !strings.Contains(buf.String(), "/proposal/p1") {
		t.Fatalf("output = %q", buf.String())
	}
}
