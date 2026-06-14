package server

import (
	"html/template"
	"net/http"
	"strconv"

	"github.com/clems4ever/granular/internal/catalog"
)

// catalogView is the data passed to the catalog page template.
type catalogView struct {
	Catalog catalog.Catalog
	Tree    []catalog.ResourceRow
	Groups  []catalog.GroupExpansion
}

// catalogPage renders the capability catalog: the resource hierarchy, the verb
// lattice, and the CLI operations, so an agent or human can see what is doable and
// how grants are scoped.
var catalogPage = template.Must(template.New("catalog").Funcs(template.FuncMap{
	"indent": func(depth int) template.CSS { return template.CSS(strconv.Itoa(depth*22) + "px") },
}).Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>granular · capabilities</title>
<style>
 body{font-family:system-ui,sans-serif;max-width:60rem;margin:2rem auto;padding:0 1rem;color:#1a1a1a;line-height:1.5}
 h1{margin-bottom:.2rem} h2{margin-top:2.2rem;border-bottom:2px solid #eee;padding-bottom:.3rem}
 .sub{color:#666;margin-top:0}
 code{background:#f4f4f4;padding:.1rem .35rem;border-radius:4px;font-size:.9em}
 .res{padding:.35rem 0;border-bottom:1px solid #f0f0f0}
 .res .name{font-weight:600}
 .chip{display:inline-block;background:#eef2ff;color:#3538cd;border-radius:10px;padding:.05rem .5rem;margin:.1rem .15rem;font-size:.8rem}
 .field{display:inline-block;background:#f4f4f4;border-radius:6px;padding:.05rem .4rem;margin:.1rem .15rem;font-size:.8rem;color:#444}
 .group{border:1px solid #e5e5e5;border-radius:8px;padding:.6rem .9rem;margin:.6rem 0}
 .group h3{margin:.1rem 0;font-family:ui-monospace,monospace}
 table{border-collapse:collapse;width:100%;margin-top:.6rem;font-size:.92rem}
 th,td{text-align:left;padding:.45rem .5rem;border-bottom:1px solid #eee;vertical-align:top}
 th{background:#fafafa}
 .rw-write{color:#cf222e;font-weight:600}
 .rw-read{color:#1f883d;font-weight:600}
 .muted{color:#999}
</style></head><body>
<h1>granular · capability catalog</h1>
<p class="sub">What the CLI can do, what can be requested, and how grants are scoped.
Machine-readable form at <code>/api/catalog</code>.</p>

<h2>Resources</h2>
<p class="sub">Typed objects, nested. A grant targets a resource (an exact object, or a class via a matcher) and an action.</p>
{{range .Tree}}
 <div class="res" style="padding-left:{{indent .Depth}}">
  <span class="name">{{.Resource.Title}}</span> &nbsp;<code>{{.Resource.Name}}</code> <span class="muted">· Cedar <code>{{.Resource.Entity}}</code></span>
  <div class="muted">{{.Resource.Description}}</div>
  {{range .Resource.Match}}<span class="field" title="{{.Description}}">{{.Name}}: {{.Type}}</span>{{end}}
 </div>
{{end}}

<h2>Verbs (action groups)</h2>
<p class="sub">Grants can name a concrete action or a group. Granting a group authorizes every action it expands to — so the agent requests <code>issue.view</code> and a <code>read</code> grant covers it.</p>
{{range .Groups}}
 <div class="group">
  <h3>{{.Group.Title}}{{if .Group.Parents}} <span class="muted" style="font-weight:400">⊂ {{range .Group.Parents}}{{.}} {{end}}</span>{{end}}</h3>
  <div class="muted">{{.Group.Description}}</div>
  <div>{{range .Actions}}<span class="chip">{{.Name}}</span>{{end}}</div>
 </div>
{{end}}

<h2>Operations</h2>
<p class="sub">Each concrete action and the CLI command that triggers it.</p>
<table>
 <tr><th>Command</th><th>Action</th><th>Resource</th><th>Kind</th><th>Grant scope</th></tr>
 {{range .Catalog.Actions}}
 <tr>
  <td>{{if .CLI}}<code>{{.CLI}}</code>{{else}}<span class="muted">(planned)</span>{{end}}<div class="muted">{{.Description}}</div></td>
  <td><code>{{.Name}}</code></td>
  <td><code>{{.Resource}}</code></td>
  <td>{{if .Mutating}}<span class="rw-write">write</span>{{else}}<span class="rw-read">read</span>{{end}}</td>
  <td>{{.Scope}}</td>
 </tr>
 {{end}}
</table>
</body></html>`))

// handleCatalogPage handles GET /catalog: it renders the capability catalog as an
// HTML page.
//
// @arg w The response writer.
// @arg r The request (unused).
//
// @testcase TestCatalogPageRenders renders the page and checks key content.
func (s *Server) handleCatalogPage(w http.ResponseWriter, r *http.Request) {
	c := catalog.Build()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = catalogPage.Execute(w, catalogView{Catalog: c, Tree: c.ResourceTree(), Groups: c.VerbGroups()})
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
