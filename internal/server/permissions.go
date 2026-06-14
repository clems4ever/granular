package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/authz"
)

// handlePermissions handles POST /api/permissions: it accepts a custom, scoped
// permissions request from the agent, translates it to Cedar policies (validated
// against the catalog), and creates a human-approvable delegation request.
//
// @arg w The response writer.
// @arg r The request whose body is an api.PermissionsRequest.
//
// @testcase TestPermissionsRequestFlow submits a request and approves it.
// @testcase TestPermissionsRequestRejectsUnknownAction returns 400 for a bad action.
func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	var req api.PermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, api.OperationResponse{Error: "invalid request body"})
		return
	}

	policies, err := authz.PoliciesFromCapabilities(authz.Principal(), req.Capabilities)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, api.OperationResponse{Error: err.Error()})
		return
	}

	dr, err := s.store.CreateRequest("permissions.request", describePermissions(req), policies, nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, api.OperationResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, api.OperationResponse{
		Status:      api.StatusPending,
		RequestID:   dr.ID,
		ApprovalURL: s.baseURL + "/approve/" + dr.ID,
	})
}

// describePermissions renders a human summary of a permissions request for the
// approval page.
//
// @arg req The permissions request.
// @return string A one-paragraph summary of the requested capabilities.
//
// @testcase TestPermissionsRequestFlow shows the summary on the approval page.
func describePermissions(req api.PermissionsRequest) string {
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
