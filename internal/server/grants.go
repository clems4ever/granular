package server

import (
	"net/http"
	"time"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/grants"
)

// grantsView is the data passed to the grants page template.
type grantsView struct {
	Grants   []grantRow
	Requests []requestRow
}

// grantRow is one active grant rendered on the grants page.
type grantRow struct {
	ID            string
	RequestID     string
	OperationType string
	Description   string
	Policy        string
	Expires       string // human "in 1m30s" style
	ExpiresAt     string // absolute expiry timestamp
	Expired       bool
}

// requestRow is one grant request rendered in the history table.
type requestRow struct {
	ID            string
	OperationType string
	Description   string
	Status        api.OperationStatus
	Created       string
	Revocable     bool // true while pending or approved
}

// collectGrants loads the active grants and request history, converting them into
// view/wire-friendly rows.
//
// @return []grantRow The active grants as rows.
// @return []requestRow The grant requests as rows.
// @error error when the store cannot be read.
//
// @testcase TestGrantsPageRenders drives this through the HTML page.
func (s *Server) collectGrants() ([]grantRow, []requestRow, error) {
	policies, err := s.store.ListGrants()
	if err != nil {
		return nil, nil, err
	}
	reqs, err := s.store.ListRequests()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	grantRows := make([]grantRow, 0, len(policies))
	for _, p := range policies {
		remaining := time.Until(p.ExpiresAt).Round(time.Second)
		grantRows = append(grantRows, grantRow{
			ID:            p.ID,
			RequestID:     p.RequestID,
			OperationType: p.OperationType,
			Description:   firstLine(p.Description),
			Policy:        p.Policy,
			Expires:       humanRemaining(remaining),
			ExpiresAt:     p.ExpiresAt.Format("2006-01-02 15:04:05"),
			Expired:       !now.Before(p.ExpiresAt),
		})
	}
	reqRows := make([]requestRow, 0, len(reqs))
	for _, r := range reqs {
		reqRows = append(reqRows, requestRow{
			ID:            r.ID,
			OperationType: r.OperationType,
			Description:   firstLine(r.Description),
			Status:        r.Status,
			Created:       r.CreatedAt.Format("2006-01-02 15:04:05"),
			Revocable:     r.Status == api.StatusPending || r.Status == api.StatusApproved,
		})
	}
	return grantRows, reqRows, nil
}

// handleGrantsPage handles GET /grants: it renders the active grants and the
// request history as an HTML page with per-grant revoke buttons.
//
// @arg w The response writer.
// @arg r The request, used to read the current session for the nav.
//
// @testcase TestGrantsPageRenders renders the grants page.
func (s *Server) handleGrantsPage(w http.ResponseWriter, r *http.Request) {
	grantRows, reqRows, err := s.collectGrants()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.render(w, r, "grants", grantsView{Grants: grantRows, Requests: reqRows})
}

// handleGrantsJSON handles GET /api/grants: it returns the active grants and the
// request history as JSON for the CLI.
//
// @arg w The response writer.
// @arg r The request (unused).
//
// @testcase TestGrantsJSONListsActiveGrants lists a grant after an approval.
func (s *Server) handleGrantsJSON(w http.ResponseWriter, r *http.Request) {
	policies, err := s.store.ListGrants()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.GrantsResponse{})
		return
	}
	reqs, err := s.store.ListRequests()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.GrantsResponse{})
		return
	}
	writeJSON(w, http.StatusOK, api.GrantsResponse{
		Grants:   grantsToAPI(policies),
		Requests: requestsToAPI(reqs),
	})
}

// handleRevoke handles POST /api/grants/{id}/revoke: it revokes the active grants
// for a grant id or a request id and reports how many were removed.
//
// @arg w The response writer.
// @arg r The request carrying the {id} path value.
//
// @testcase TestRevokeEndpointRemovesGrant revokes a grant and checks it is gone.
// @testcase TestRevokePendingRequestEndpoint revokes a pending request by its id.
func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, found, err := s.store.Revoke(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.RevokeResponse{Error: err.Error()})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, api.RevokeResponse{Error: "no grant or request for that id"})
		return
	}
	writeJSON(w, http.StatusOK, api.RevokeResponse{Revoked: n})
}

// handleRevokeForm handles POST /grants/{id}/revoke from the web page: it revokes
// the grant and redirects back to the grants page.
//
// @arg w The response writer.
// @arg r The request carrying the {id} path value.
//
// @testcase TestRevokeFormRedirects revokes via the form and redirects.
func (s *Server) handleRevokeForm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, _, err := s.store.Revoke(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/grants", http.StatusSeeOther)
}

// grantsToAPI converts stored policies into the wire grant list.
//
// @arg policies The stored policies.
// @return []api.Grant The wire representation.
//
// @testcase TestGrantsJSONListsActiveGrants decodes this output.
func grantsToAPI(policies []grants.Grant) []api.Grant {
	out := make([]api.Grant, 0, len(policies))
	for _, p := range policies {
		out = append(out, api.Grant{
			ID:            p.ID,
			RequestID:     p.RequestID,
			OperationType: p.OperationType,
			Description:   p.Description,
			Policy:        p.Policy,
			CreatedAt:     p.CreatedAt.Format(time.RFC3339),
			ExpiresAt:     p.ExpiresAt.Format(time.RFC3339),
			Status:        api.StatusApproved,
		})
	}
	return out
}

// requestsToAPI converts grant requests into the wire request list.
//
// @arg reqs The grant requests.
// @return []api.GrantRequestInfo The wire representation.
//
// @testcase TestGrantsJSONListsActiveGrants decodes this output.
func requestsToAPI(reqs []grants.GrantRequest) []api.GrantRequestInfo {
	out := make([]api.GrantRequestInfo, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, api.GrantRequestInfo{
			ID:            r.ID,
			OperationType: r.OperationType,
			Description:   firstLine(r.Description),
			Status:        r.Status,
			CreatedAt:     r.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

// firstLine returns the first line of s, trimmed, for compact table display.
//
// @arg s The (possibly multi-line) string.
// @return string The first line.
//
// @testcase TestFirstLine returns the first line of a multi-line string.
func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// humanRemaining renders a remaining duration for display, clamping negatives to
// "expired".
//
// @arg d The remaining duration.
// @return string A human string such as "1m30s" or "expired".
//
// @testcase TestHumanRemaining formats positive and negative durations.
func humanRemaining(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	return d.String()
}
