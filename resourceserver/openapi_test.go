package resourceserver

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// openapiPathParam matches an OpenAPI path templating segment, e.g. "{token}".
var openapiPathParam = regexp.MustCompile(`\{[^}]+\}`)

// TestOpenAPISpecValid checks the embedded RS spec is a valid OpenAPI 3 document.
func TestOpenAPISpecValid(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(OpenAPISpec())
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("invalid OpenAPI spec: %v", err)
	}
}

// TestOpenAPIRoutesExist checks every path+method documented in the spec is actually
// routed by the resource server, guarding against spec/code drift.
func TestOpenAPIRoutesExist(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(OpenAPISpec())
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	ts := httptest.NewServer(New(Config{}).Handler())
	t.Cleanup(ts.Close)

	for path, item := range doc.Paths.Map() {
		concrete := openapiPathParam.ReplaceAllString(path, "x")
		for method := range item.Operations() {
			req, err := http.NewRequest(method, ts.URL+concrete, nil)
			if err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
				t.Errorf("documented %s %s is not routed (status %d)", method, path, resp.StatusCode)
			}
		}
	}
}

// TestOpenAPIServed serves the spec at GET /openapi.yaml as YAML.
func TestOpenAPIServed(t *testing.T) {
	ts := httptest.NewServer(New(Config{}).Handler())
	t.Cleanup(ts.Close)
	resp, err := http.Get(ts.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /openapi.yaml = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "yaml") {
		t.Fatalf("content-type = %q, want yaml", ct)
	}
}
