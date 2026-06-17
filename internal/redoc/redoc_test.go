package redoc

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRegisterServesDocsAndScript checks the docs page references the spec and the embedded
// bundle, and that the bundle is served as JavaScript.
func TestRegisterServesDocsAndScript(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, "granular API", "/openapi.yaml")
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// The docs page renders and points Redoc at the spec and the embedded bundle.
	resp, err := http.Get(ts.URL + "/docs")
	if err != nil {
		t.Fatalf("GET /docs: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /docs = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want html", ct)
	}
	for _, want := range []string{`spec-url="/openapi.yaml"`, scriptPath, "granular API"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("docs page missing %q:\n%s", want, body)
		}
	}

	// The embedded Redoc bundle is served as JavaScript.
	resp, err = http.Get(ts.URL + scriptPath)
	if err != nil {
		t.Fatalf("GET %s: %v", scriptPath, err)
	}
	js, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", scriptPath, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type = %q, want javascript", ct)
	}
	if len(js) < 1000 {
		t.Fatalf("embedded bundle suspiciously small: %d bytes", len(js))
	}
}
