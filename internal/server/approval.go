package server

import (
	"net/http"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/server/web"
)

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
	_ = web.Render(w, "approve", view)
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

	_ = web.Render(w, "result", struct {
		Status  api.OperationStatus
		Message string
	}{Status: status, Message: message})
}
