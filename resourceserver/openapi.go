package resourceserver

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var openapiSpec []byte

// OpenAPISpec returns the embedded OpenAPI document describing the resource server's
// JSON API.
//
// @return []byte The OpenAPI specification as YAML.
//
// @testcase TestOpenAPISpecValid loads and validates the served spec.
func OpenAPISpec() []byte { return openapiSpec }

// handleOpenAPI serves the embedded OpenAPI document describing the resource server API.
//
// @arg w The response writer.
// @arg r The incoming request.
//
// @testcase TestOpenAPIServed serves the spec over HTTP.
func (g *ResourceServer) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(openapiSpec)
}
