// Package redoc serves a browsable Redoc UI for an OpenAPI document. The Redoc bundle is
// embedded in the binary, so the granular servers render API docs at /docs with no
// external or CDN dependency at view time.
package redoc

import (
	_ "embed"
	"fmt"
	"html"
	"net/http"
)

//go:embed redoc.standalone.js
var script []byte

// scriptPath is where the embedded bundle is served, referenced by the docs page.
const scriptPath = "/docs/redoc.standalone.js"

// Register mounts the Redoc docs UI on mux: GET /docs renders a page for the OpenAPI
// document at specURL, and GET /docs/redoc.standalone.js serves the embedded bundle. No
// network access is needed to view it — the bundle ships inside the binary.
//
// @arg mux The mux to register the docs routes on.
// @arg title The browser title for the docs page.
// @arg specURL The path the page tells Redoc to load the spec from, e.g. "/openapi.yaml".
//
// @testcase TestRegisterServesDocsAndScript serves both the page and the bundle.
func Register(mux *http.ServeMux, title, specURL string) {
	doc := page(title, specURL)
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(doc)
	})
	mux.HandleFunc("GET "+scriptPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(script)
	})
}

// page builds the Redoc HTML page for the spec at specURL, loading the embedded bundle.
//
// @arg title The browser title.
// @arg specURL The spec location Redoc loads.
// @return []byte The HTML document.
//
// @testcase TestRegisterServesDocsAndScript renders a page referencing the spec.
func page(title, specURL string) []byte {
	return []byte(fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
</head>
<body>
<redoc spec-url="%s"></redoc>
<script src="%s"></script>
</body>
</html>
`, html.EscapeString(title), html.EscapeString(specURL), scriptPath))
}
