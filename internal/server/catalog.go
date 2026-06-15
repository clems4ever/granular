package server

import (
	"encoding/json"
	"net/http"

	"github.com/clems4ever/granular/internal/catalog"
)

// catalogView is the data passed to the catalog page template.
type catalogView struct {
	Catalog     catalog.Catalog
	Tree        []catalog.ResourceRow
	Groups      []catalog.GroupExpansion
	ExampleJSON string
}

// handleIndex handles GET /: it renders the landing page.
//
// @arg w The response writer.
// @arg r The request, used to read the current session for the nav.
//
// @testcase TestIndexPageRenders renders the landing page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "index", nil)
}

// handleCatalogPage handles GET /catalog: it renders the capability catalog as an
// HTML page.
//
// @arg w The response writer.
// @arg r The request, used to read the current session for the nav.
//
// @testcase TestCatalogPageRenders renders the page and checks key content.
func (s *Server) handleCatalogPage(w http.ResponseWriter, r *http.Request) {
	c := catalog.Build()
	example, _ := json.MarshalIndent(c.RequestExample, "", "  ")
	_ = s.render(w, r, "catalog", catalogView{
		Catalog:     c,
		Tree:        c.ResourceTree(),
		Groups:      c.VerbGroups(),
		ExampleJSON: string(example),
	})
}

// handleCatalogJSON handles GET /api/catalog: it returns the capability manifest
// as JSON for programmatic consumption (e.g. by the agent).
//
// @arg w The response writer.
// @arg r The request (unused).
//
// @testcase TestCatalogJSON returns the manifest with resources and actions.
func (s *Server) handleCatalogJSON(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, catalog.Build())
}
