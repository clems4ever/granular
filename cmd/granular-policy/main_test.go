package main

import (
	"bytes"
	"context"
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
	mux.HandleFunc("GET /api/policy/{token}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": []client.Grant{{GatewayID: "g1", ExpiresAt: "soon"}}})
	})
	mux.HandleFunc("DELETE /api/policy/{token}", func(w http.ResponseWriter, r *http.Request) {
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

// TestClientRequiresAdminToken errors when no admin token is configured.
func TestClientRequiresAdminToken(t *testing.T) {
	if _, err := (&admin{asURL: "http://as"}).client(); err == nil {
		t.Fatal("expected an error without an admin token")
	}
	if _, err := (&admin{asURL: "http://as", adminToken: "x"}).client(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunPolicy creates a token, lists grants, and destroys a policy via the AS.
func TestRunPolicy(t *testing.T) {
	as := fakeAS(t)
	c := client.New(client.Config{ASURL: as.URL, Token: "admin"})

	var buf bytes.Buffer
	if err := runCreate(context.Background(), c, &buf); err != nil || !strings.Contains(buf.String(), "tok") {
		t.Fatalf("create: %v %q", err, buf.String())
	}

	buf.Reset()
	if err := runShow(context.Background(), c, "somepolicy", &buf); err != nil || !strings.Contains(buf.String(), "g1") {
		t.Fatalf("show: %v %q", err, buf.String())
	}

	buf.Reset()
	if err := runDestroy(context.Background(), c, "somepolicy", &buf); err != nil || !strings.Contains(buf.String(), "destroyed 3") {
		t.Fatalf("destroy: %v %q", err, buf.String())
	}
}
