package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/authz"
)

// handleCapabilityRequest handles the capability path of POST /api/requests: it
// translates a scoped capability bundle to Cedar policies (validated against the
// catalog) and creates a human-approvable grant request. Unlike the operation
// path it never executes anything — it only pre-approves access.
//
// @arg w The response writer.
// @arg req The grant request carrying the capability bundle.
//
// @testcase TestPermissionsRequestFlow submits a request and approves it.
// @testcase TestPermissionsRequestRejectsUnknownAction returns 400 for a bad action.
func (s *Server) handleCapabilityRequest(w http.ResponseWriter, req api.GrantRequest) {
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
