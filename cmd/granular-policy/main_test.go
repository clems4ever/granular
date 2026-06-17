package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/client"
)

// fakeAS is a minimal AS exposing the policy endpoints for the admin CLI tests.
//
// @arg t The test handle.
// @return *httptest.Server The running fake AS.
//
// @testcase TestRunPolicy drives this AS.
func fakeAS(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/policy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
	})
	mux.HandleFunc("GET /api/policy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": []client.Grant{{GatewayID: "g1", ExpiresAt: "soon"}}})
	})
	mux.HandleFunc("DELETE /api/policy", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"destroyed": 3})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// TestMainIsEntryPoint is a placeholder: main only builds and executes the tree.
func TestMainIsEntryPoint(t *testing.T) { _ = main }

// TestCommandTree checks the create/show/destroy sub-commands are wired.
func TestCommandTree(t *testing.T) {
	root := newRootCmd(&bytes.Buffer{})
	want := map[string]bool{"create": true, "show": true, "destroy": true}
	for _, c := range root.Commands() {
		delete(want, c.Name())
	}
	if len(want) != 0 {
		t.Fatalf("missing commands: %v", want)
	}
}

// TestRunPolicy creates a token, lists grants, and destroys the policy via the AS.
func TestRunPolicy(t *testing.T) {
	as := fakeAS(t)

	var buf bytes.Buffer
	create := &admin{asURL: as.URL, out: &buf}
	if err := create.run(runCreate); err != nil || !strings.Contains(buf.String(), "tok") {
		t.Fatalf("create: %v %q", err, buf.String())
	}

	buf.Reset()
	withTok := &admin{asURL: as.URL, token: "tok", out: &buf}
	if err := withTok.run(runShow); err != nil || !strings.Contains(buf.String(), "g1") {
		t.Fatalf("show: %v %q", err, buf.String())
	}

	buf.Reset()
	if err := withTok.run(runDestroy); err != nil || !strings.Contains(buf.String(), "destroyed 3") {
		t.Fatalf("destroy: %v %q", err, buf.String())
	}

	// show/destroy require a token.
	noTok := &admin{asURL: as.URL, out: &bytes.Buffer{}}
	if err := noTok.run(runShow); err == nil {
		t.Fatal("expected show to require a token")
	}
	if err := noTok.run(runDestroy); err == nil {
		t.Fatal("expected destroy to require a token")
	}
}
