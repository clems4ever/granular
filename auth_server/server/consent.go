package server

import (
	"net/http"
	"time"

	"github.com/clems4ever/granular/auth_server/store"
	"github.com/clems4ever/granular/internal/proposal"
)

// defaultTTL is the grant lifetime used when the approval form omits or sends an
// invalid duration. It is short by design so grants expire quickly.
const defaultTTL = 2 * time.Minute

// ttlOption is one expiration choice offered on the consent page.
type ttlOption struct {
	Label string
	Value string
}

// ttlOptions lists the expiration choices; the first entry is the default selected.
var ttlOptions = []ttlOption{
	{"2 minutes", "2m"},
	{"15 minutes", "15m"},
	{"1 hour", "1h"},
	{"8 hours", "8h"},
	{"24 hours", "24h"},
}

// itemView is one gateway-signed grant request rendered on the consent page. Every
// field is taken verbatim from the gateway-authored Presentation; the AS adds no
// meaning of its own. Policies are the raw, opaque blobs, shown collapsed.
type itemView struct {
	GatewayID string
	Title     string
	Summary   string
	Detail    string
	Grants    []proposal.GrantDetail
	Policies  []string
}

// approvalView is the data passed to the consent page template.
type approvalView struct {
	ID         string
	Approver   string
	Items      []itemView
	Decided    bool
	Status     store.Status
	TTLOptions []ttlOption
}

// mismatchView is rendered when the signed-in user is not the named approver.
type mismatchView struct {
	SignedInAs string
	Approver   string
}

// parseTTL converts a consent-form duration value into a time.Duration, falling back
// to defaultTTL (2 minutes) for empty or invalid input.
//
// @arg value The raw duration string from the form, e.g. "2m".
// @return time.Duration The parsed duration, or defaultTTL on failure.
//
// @testcase TestParseTTLFallsBack checks empty and invalid values default to 2m.
func parseTTL(value string) time.Duration {
	if value == "" {
		return defaultTTL
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return defaultTTL
	}
	return d
}

// viewItems maps a proposal's signed items into the verbatim display structs.
//
// @arg p The proposal whose items are rendered.
// @return []itemView One view per item, presentation copied as-is.
//
// @testcase TestApprovePageRendersItems renders a proposal's items.
func viewItems(p *store.Proposal) []itemView {
	out := make([]itemView, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, itemView{
			GatewayID: it.GatewayID,
			Title:     it.Presentation.Title,
			Summary:   it.Presentation.Summary,
			Detail:    it.Presentation.Detail,
			Grants:    it.Presentation.Grants,
			Policies:  it.Policies,
		})
	}
	return out
}

// approverGate returns ("", true) to proceed, or renders a denial and returns false,
// enforcing that the signed-in user's email equals the proposal's approver. When
// authentication is disabled it proceeds (the operator runs an open server).
//
// @arg w The response writer (used to render a denial).
// @arg r The request, used to read the current session.
// @arg p The proposal whose approver email gates the decision.
// @return bool True when the caller may view/decide the proposal.
//
// @testcase TestApproveDeniesWrongApprover blocks a non-approver.
func (s *Server) approverGate(w http.ResponseWriter, r *http.Request, p *store.Proposal) bool {
	if s.auth == nil || !s.auth.Enabled() {
		return true
	}
	email, ok := s.auth.CurrentEmail(r)
	if ok && email == p.ApproverEmail {
		return true
	}
	w.WriteHeader(http.StatusForbidden)
	_ = s.render(w, r, "denied", mismatchView{SignedInAs: email, Approver: p.ApproverEmail})
	return false
}

// handleApprovePage handles GET /proposal/{id}: it renders the consent form for the
// named approver, or a notice when the proposal was already decided.
//
// @arg w The response writer.
// @arg r The request carrying the {id} path value.
//
// @testcase TestApprovePageRendersItems renders a pending proposal's items.
// @testcase TestApprovePageNotFound returns 404 for an unknown id.
func (s *Server) handleApprovePage(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProposal(r.PathValue("id"))
	if err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}
	if !s.approverGate(w, r, p) {
		return
	}
	_ = s.render(w, r, "approve", approvalView{
		ID:         p.ID,
		Approver:   p.ApproverEmail,
		Items:      viewItems(p),
		Decided:    p.Status != store.StatusPending,
		Status:     p.Status,
		TTLOptions: ttlOptions,
	})
}

// handleApproveSubmit handles POST /proposal/{id}: it records the named approver's
// approve or reject decision and renders a confirmation.
//
// @arg w The response writer.
// @arg r The request whose form carries "decision" and "ttl".
//
// @testcase TestProposalApproveFlow approves via this endpoint.
// @testcase TestApproveSubmitReject rejects a proposal through this endpoint.
func (s *Server) handleApproveSubmit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.store.GetProposal(id)
	if err != nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}
	if !s.approverGate(w, r, p) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	var (
		status  store.Status
		message string
	)
	switch r.FormValue("decision") {
	case "approve":
		if _, err = s.store.Approve(id, parseTTL(r.FormValue("ttl"))); err == nil {
			status = store.StatusApproved
			message = "The request has been approved. You can return to your terminal."
		}
	case "reject":
		if _, err = s.store.Reject(id); err == nil {
			status = store.StatusRejected
			message = "The request has been rejected."
		}
	default:
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	_ = s.render(w, r, "result", struct {
		Status  store.Status
		Message string
	}{Status: status, Message: message})
}
