package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clems4ever/granular/internal/api"
)

func TestSubmitDecodesResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"pending","request_id":"abc","approval_url":"http://x/approve/abc"}`))
	}))
	defer ts.Close()

	c := New(ts.URL)
	resp, err := c.Submit(context.Background(), api.OperationRequest{Type: "test.op"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != api.StatusPending || resp.RequestID != "abc" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRequestPermissionsDecodesResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/permissions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"pending","request_id":"p1","approval_url":"http://x/approve/p1"}`))
	}))
	defer ts.Close()

	resp, err := New(ts.URL).RequestPermissions(context.Background(), api.PermissionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.RequestID != "p1" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCatalogFetchesManifest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resources":[]}`))
	}))
	defer ts.Close()

	body, err := New(ts.URL).Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"resources":[]}` {
		t.Fatalf("unexpected catalog body: %s", body)
	}
}

func TestGrantsAndRevoke(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/grants", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"grants":[{"id":"g1","operation_type":"github.clone"}],"requests":[]}`))
	})
	mux.HandleFunc("POST /api/grants/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"revoked":1}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := New(ts.URL)
	g, err := c.Grants(context.Background())
	if err != nil || len(g.Grants) != 1 || g.Grants[0].ID != "g1" {
		t.Fatalf("Grants = %+v, %v", g, err)
	}
	rv, err := c.Revoke(context.Background(), "g1")
	if err != nil || rv.Revoked != 1 {
		t.Fatalf("Revoke = %+v, %v", rv, err)
	}
}
