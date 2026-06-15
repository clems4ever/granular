package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/authz"
)

// handleGrantRequest handles POST /api/grant-requests: an agent asks to be granted
// a bundle of capabilities for later use. It translates the scoped capabilities to
// Cedar policies (validated against the catalog) and records a human-approvable
// grant request. Unlike an operation it never executes anything — it only
// pre-approves access. An empty capability bundle is rejected with 400.
//
// @arg w The response writer.
// @arg r The request whose body is an api.GrantRequest.
//
// @testcase TestPermissionsRequestFlow submits a request and approves it.
// @testcase TestPermissionsRequestRejectsUnknownAction returns 400 for a bad action.
// @testcase TestGrantRequestWithoutCapabilities returns 400 for an empty bundle.
func (s *Server) handleGrantRequest(w http.ResponseWriter, r *http.Request) {
	var req api.GrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.RequestResponse{Error: "invalid request body"})
		return
	}
	if len(req.Capabilities) == 0 {
		writeJSON(w, http.StatusBadRequest, api.RequestResponse{Error: "grant request must name at least one capability"})
		return
	}
	policies, err := authz.PoliciesFromCapabilities(authz.Principal(), req.Capabilities)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, api.RequestResponse{Error: err.Error()})
		return
	}

	dr, err := s.store.CreateRequest("capability.request", describePermissions(req), policies, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.RequestResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, api.RequestResponse{
		Status:      api.StatusPending,
		RequestID:   dr.ID,
		ApprovalURL: s.baseURL + "/approve/" + dr.ID,
	})
}

// describePermissions renders a human summary of a grant request for the
// approval page.
//
// @arg req The grant request.
// @return string A one-paragraph summary of the requested capabilities.
//
// @testcase TestPermissionsRequestFlow shows the summary on the approval page.
func describePermissions(req api.GrantRequest) string {
	var lines []string
	if req.Reason != "" {
		lines = append(lines, "Reason: "+req.Reason)
	}
	for _, c := range req.Capabilities {
		match := make([]string, 0, len(c.Resource.Match))
		for k, v := range c.Resource.Match {
			match = append(match, k+"="+v)
		}
		lines = append(lines, fmt.Sprintf("Allow [%s] on %s {%s}",
			strings.Join(c.Actions, ", "), c.Resource.Type, strings.Join(match, ", ")))
	}
	return strings.Join(lines, "\n")
}
