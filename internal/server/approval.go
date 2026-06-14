package server

import (
	"html/template"
	"net/http"

	"github.com/clems4ever/granular/internal/api"
)

// approvalPage renders the form a human uses to approve or reject a pending
// delegation request.
var approvalPage = template.Must(template.New("approve").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>granular · approve</title>
<style>
 body{font-family:system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem;color:#1a1a1a}
 .card{border:1px solid #ddd;border-radius:8px;padding:1.5rem}
 code{background:#f4f4f4;padding:.1rem .3rem;border-radius:4px}
 .desc{font-size:1.1rem;margin:.5rem 0 1.5rem}
 label{display:block;margin:.75rem 0 .25rem;font-weight:600}
 select,button{font-size:1rem;padding:.5rem;border-radius:6px}
 .actions{margin-top:1.5rem;display:flex;gap:.75rem}
 .approve{background:#1f883d;color:#fff;border:none}
 .reject{background:#fff;color:#cf222e;border:1px solid #cf222e}
 .status{font-weight:600}
</style></head><body>
<div class="card">
 <h1>Approve operation</h1>
 {{if .Decided}}
   <p>This request has already been <span class="status">{{.Status}}</span>.</p>
 {{else}}
   <p class="desc" style="white-space:pre-line">{{.Description}}</p>
   <p>Operation: <code>{{.OperationType}}</code></p>
   {{if .Policies}}<details><summary>Cedar policies this grants</summary>
     <pre style="background:#f4f4f4;padding:.75rem;border-radius:6px;overflow:auto">{{range .Policies}}{{.}}

{{end}}</pre></details>{{end}}
   <form method="post" action="/approve/{{.ID}}">
     <label for="ttl">Grant valid for</label>
     <select id="ttl" name="ttl">
       {{range .TTLOptions}}<option value="{{.Value}}">{{.Label}}</option>{{end}}
     </select>
     <div class="actions">
       <button class="approve" type="submit" name="decision" value="approve">Approve</button>
       <button class="reject" type="submit" name="decision" value="reject">Reject</button>
     </div>
   </form>
 {{end}}
</div></body></html>`))

// resultPage renders the confirmation shown after a human decision.
var resultPage = template.Must(template.New("result").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>granular · {{.Status}}</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1rem}
 .card{border:1px solid #ddd;border-radius:8px;padding:1.5rem}</style></head><body>
<div class="card"><h1>Request {{.Status}}</h1><p>{{.Message}}</p></div>
</body></html>`))

// approvalView is the data passed to the approval page template.
type approvalView struct {
	ID            string
	OperationType string
	Description   string
	Policies      []string
	Status        api.OperationStatus
	Decided       bool
	TTLOptions    []struct {
		Label string
		Value string
	}
}

// handleApprovePage handles GET /approve/{id}: it renders the approval form, or a
// notice when the request was already decided.
//
// @arg w The response writer.
// @arg r The request carrying the {id} path value.
//
// @testcase TestApprovePageRendersForm renders a pending request's form.
// @testcase TestApprovePageNotFound returns 404 for an unknown id.
func (s *Server) handleApprovePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dr, err := s.store.GetRequest(id)
	if err != nil {
		http.Error(w, "delegation request not found", http.StatusNotFound)
		return
	}
	view := approvalView{
		ID:            dr.ID,
		OperationType: dr.OperationType,
		Description:   dr.Description,
		Policies:      dr.ProposedPolicies,
		Status:        dr.Status,
		Decided:       dr.Status != api.StatusPending,
		TTLOptions:    ttlOptions,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = approvalPage.Execute(w, view)
}

// handleApproveSubmit handles POST /approve/{id}: it records the human's approve
// or reject decision and renders a confirmation.
//
// @arg w The response writer.
// @arg r The request whose form carries "decision" and "ttl".
//
// @testcase TestOperationPendingThenApprovedThenCompleted approves via this endpoint.
// @testcase TestApproveSubmitReject rejects a request through this endpoint.
func (s *Server) handleApproveSubmit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	var (
		status  api.OperationStatus
		message string
		err     error
	)
	switch r.FormValue("decision") {
	case "approve":
		ttl := parseTTL(r.FormValue("ttl"))
		if _, err = s.store.Approve(id, ttl); err == nil {
			status = api.StatusApproved
			message = "The operation has been approved. You can return to your terminal."
		}
	case "reject":
		if _, err = s.store.Reject(id); err == nil {
			status = api.StatusRejected
			message = "The operation has been rejected."
		}
	default:
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = resultPage.Execute(w, struct {
		Status  api.OperationStatus
		Message string
	}{Status: status, Message: message})
}
