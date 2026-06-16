// Package web holds the embedded HTML templates and static assets for the granular
// authorization server's consent UI, and renders pages against a shared layout.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html static/*
var files embed.FS

// pages maps a page name to its parsed template (layout + page).
var pages = map[string]*template.Template{
	"approve": page("approve.html"),
	"result":  page("result.html"),
	"denied":  page("denied.html"),
}

// page parses the shared layout together with a single page template.
//
// @arg name The page template filename, e.g. "approve.html".
// @return *template.Template The parsed template set rooted at the layout.
//
// @testcase TestRenderProducesHTML renders every page.
func page(name string) *template.Template {
	return template.Must(template.New("layout.html").ParseFS(files, "templates/layout.html", "templates/"+name))
}

// Nav holds the per-request layout chrome: whether the consent UI is behind a login
// and, if so, the signed-in GitHub user shown in the top bar.
type Nav struct {
	User        string
	AuthEnabled bool
}

// layoutData wraps a page's own data with the shared layout chrome (Nav). The
// layout passes Page to each page template (as dot), so page templates are
// unaffected, while the top bar renders Nav.
type layoutData struct {
	Nav  Nav
	Page any
}

// Render writes the named page, executed against the shared layout, to w. nav is
// the layout chrome (the signed-in user); data is the page's own template data.
//
// @arg w The response writer; its content type is set to HTML.
// @arg name The page name (key of pages), e.g. "approve".
// @arg nav The layout chrome (signed-in user / whether auth is enabled).
// @arg data The data passed to the page template.
// @error error when the page is unknown or template execution fails.
//
// @testcase TestRenderProducesHTML renders a page and checks the output.
// @testcase TestRenderUnknownPage returns an error for an unknown page.
func Render(w http.ResponseWriter, name string, nav Nav, data any) error {
	tmpl, ok := pages[name]
	if !ok {
		return fs.ErrNotExist
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return tmpl.ExecuteTemplate(w, "layout.html", layoutData{Nav: nav, Page: data})
}

// Static serves the embedded static assets, stripped of the /static/ prefix.
//
// @return http.Handler A handler serving the embedded static/ directory.
//
// @testcase TestStaticServesCSS fetches the stylesheet.
func Static() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
