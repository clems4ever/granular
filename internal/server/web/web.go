// Package web holds the embedded HTML templates and static assets for the granular
// server's UI, and renders pages against a shared layout.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
)

//go:embed templates/*.html static/*
var files embed.FS

// funcs are the template helpers shared by every page.
var funcs = template.FuncMap{
	"indent": func(depth int) template.CSS { return template.CSS(strconv.Itoa(depth*20) + "px") },
}

// pages maps a page name to its parsed template (layout + page).
var pages = map[string]*template.Template{
	"index":   page("index.html"),
	"catalog": page("catalog.html"),
	"approve": page("approve.html"),
	"result":  page("result.html"),
}

// page parses the shared layout together with a single page template.
//
// @arg name The page template filename, e.g. "approve.html".
// @return *template.Template The parsed template set rooted at the layout.
//
// @testcase TestRenderProducesHTML renders every page.
func page(name string) *template.Template {
	return template.Must(template.New("layout.html").Funcs(funcs).ParseFS(files, "templates/layout.html", "templates/"+name))
}

// Render writes the named page, executed against the shared layout, to w.
//
// @arg w The response writer; its content type is set to HTML.
// @arg name The page name (key of pages), e.g. "approve".
// @arg data The data passed to the template.
// @error error when the page is unknown or template execution fails.
//
// @testcase TestRenderProducesHTML renders a page and checks the output.
// @testcase TestRenderUnknownPage returns an error for an unknown page.
func Render(w http.ResponseWriter, name string, data any) error {
	tmpl, ok := pages[name]
	if !ok {
		return fs.ErrNotExist
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return tmpl.ExecuteTemplate(w, "layout.html", data)
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
