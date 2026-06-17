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

// fakeAS is a minimal AS exposing the subject endpoints for the admin CLI tests.
//
// @arg t The test handle.
// @return *httptest.Server The running fake AS.
//
// @testcase TestRunSubject drives this AS.
func fakeAS(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/subject", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
	})
	mux.HandleFunc("GET /api/subject/{token}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"grants": []client.Grant{{ResourceServerID: "g1", ExpiresAt: "soon"}}})
	})
	mux.HandleFunc("DELETE /api/subject/{token}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"destroyed": 3})
	})
	mux.HandleFunc("GET /api/activity", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Activity{
			Grants:  []client.Grant{{SubjectToken: "subj1", ResourceServerID: "g1", ExpiresAt: "soon"}},
			History: []client.HistoryEntry{{SubjectToken: "subj1", Approver: "me@example.com", Status: "approved", Summary: "do x", Items: 1}},
		})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// TestMainIsEntryPoint is a placeholder: main only builds and executes the tree.
func TestMainIsEntryPoint(t *testing.T) { _ = main }

// TestCommandTree checks the create/show/destroy/activity sub-commands are wired.
func TestCommandTree(t *testing.T) {
	root := newRootCmd(&bytes.Buffer{})
	want := map[string]bool{"create": true, "show": true, "destroy": true, "activity": true}
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

// TestRunSubject creates a token, lists grants, and destroys a subject via the AS.
func TestRunSubject(t *testing.T) {
	as := fakeAS(t)
	c := client.New(client.Config{ASURL: as.URL, Token: "admin"})

	var buf bytes.Buffer
	if err := runCreate(context.Background(), c, &buf); err != nil || !strings.Contains(buf.String(), "tok") {
		t.Fatalf("create: %v %q", err, buf.String())
	}

	buf.Reset()
	if err := runShow(context.Background(), c, "somesubject", &buf); err != nil || !strings.Contains(buf.String(), "g1") {
		t.Fatalf("show: %v %q", err, buf.String())
	}

	buf.Reset()
	if err := runDestroy(context.Background(), c, "somesubject", &buf); err != nil || !strings.Contains(buf.String(), "destroyed 3") {
		t.Fatalf("destroy: %v %q", err, buf.String())
	}
}

// TestRunActivity prints the cross-subject grant inventory and request history.
func TestRunActivity(t *testing.T) {
	as := fakeAS(t)
	c := client.New(client.Config{ASURL: as.URL, Token: "admin"})

	var buf bytes.Buffer
	if err := runActivity(context.Background(), c, &buf); err != nil {
		t.Fatalf("activity: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"subj1", "g1", "me@example.com", "approved"} {
		if !strings.Contains(out, want) {
			t.Fatalf("activity output missing %q:\n%s", want, out)
		}
	}
}
