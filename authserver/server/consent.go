package server

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/clems4ever/granular/authserver/store"
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

// itemView is one resource server-signed grant request rendered on the consent page. Every
// field is taken verbatim from the resource server-authored Presentation; the AS adds no
// meaning of its own. Policies are the raw, opaque blobs, shown collapsed.
type itemView struct {
	ResourceServerID string
	Title            string
	Summary          string
	Detail           string
	Grants           []proposal.GrantDetail
	Policies         []string
}

// approvalView is the data passed to the consent page template.
type approvalView struct {
	ID         string
	Approver   string
	Items      []itemView
	Decided    bool
	Status     store.Status
	ExpiresIn  string // relative time until the pending request is auto-revoked
	TTLOptions []ttlOption
}

// mismatchView is rendered when the signed-in user is not the named approver.
type mismatchView struct {
	SignedInAs string
	Approver   string
}

// historyRow is one past or pending proposal shown on the approver's activity page: the
// status, a one-line summary and when it was submitted. The approver is implicit (always
// the signed-in user), so it is not repeated per row.
type historyRow struct {
	Status    store.Status
	Summary   string
	Items     int
	CreatedAt string
}

// activityView is the data passed to the activity page template: the signed-in approver's
// own request/decision history.
type activityView struct {
	History []historyRow
}

// fmtTime renders a timestamp in a compact UTC form for the UI, or "—" for the zero time.
//
// @arg t The timestamp to render.
// @return string The formatted UTC timestamp.
//
// @testcase TestHomeShowsApproverHistory renders proposal timestamps.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// humanizeUntil renders how long until t, relative to now, as a short phrase like
// "in 2h 5m", "in 12m", "in 30s", or "expired" once t has passed.
//
// @arg now The reference time.
// @arg t The future time to describe.
// @return string The relative time-to-expiry phrase.
//
// @testcase TestHumanizeUntil covers hours, minutes, seconds and the expired case.
func humanizeUntil(now, t time.Time) string {
	d := t.Sub(now)
	switch {
	case d <= 0:
		return "expired"
	case d >= time.Hour:
		return fmt.Sprintf("in %dh %dm", int(d/time.Hour), int((d%time.Hour)/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("in %dm", int(d/time.Minute))
	default:
		return fmt.Sprintf("in %ds", int(d/time.Second))
	}
}

// firstSummary returns a representative summary for a bundle of signed items: the first
// item's summary, suffixed when more items are present.
//
// @arg items The signed grant requests in a proposal.
// @return string A one-line summary for the bundle.
//
// @testcase TestHomeShowsApproverHistory summarises a multi-item proposal.
func firstSummary(items []proposal.SignedGrantRequest) string {
	if len(items) == 0 {
		return ""
	}
	s := items[0].Presentation.Summary
	if len(items) > 1 {
		s += " (+" + itoa(len(items)-1) + " more)"
	}
	return s
}

// buildActivity assembles the signed-in approver's activity view from their own proposals:
// one history row each, with lapsed pending requests shown as expired.
//
// @arg now The reference time used to mark lapsed pending requests as expired.
// @arg proposals The approver's own proposals (already filtered by handleHome).
// @return activityView The view rendered by the activity page.
//
// @testcase TestHomeShowsApproverHistory builds the history section.
func buildActivity(now time.Time, proposals []store.Proposal) activityView {
	v := activityView{}
	for _, p := range proposals {
		status := p.Status
		if p.Expired(now) {
			status = store.StatusExpired
		}
		v.History = append(v.History, historyRow{
			Status:    status,
			Summary:   firstSummary(p.Items),
			Items:     len(p.Items),
			CreatedAt: fmtTime(p.CreatedAt),
		})
	}
	return v
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
			ResourceServerID: it.ResourceServerID,
			Title:            it.Presentation.Title,
			Summary:          it.Presentation.Summary,
			Detail:           it.Presentation.Detail,
			Grants:           it.Presentation.Grants,
			Policies:         it.Policies,
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
	now := time.Now()
	status := p.Status
	if p.Expired(now) {
		status = store.StatusExpired
	}
	_ = s.render(w, r, "approve", approvalView{
		ID:         p.ID,
		Approver:   p.ApproverEmail,
		Items:      viewItems(p),
		Decided:    status != store.StatusPending,
		Status:     status,
		ExpiresIn:  humanizeUntil(now, p.ExpiresAt),
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
	if errors.Is(err, store.ErrExpired) {
		_ = s.render(w, r, "result", struct {
			Status  store.Status
			Message string
		}{Status: store.StatusExpired, Message: "This request expired before it was decided. Ask the agent to submit a new one."})
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
