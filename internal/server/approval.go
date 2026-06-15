package server

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/catalog"
)

// grantedAction is a plain-English description of one action a request would
// grant, resolved from the catalog so the approver does not have to read Cedar.
type grantedAction struct {
	Name        string
	Title       string
	Description string
	Kind        string // "read", "write", or "group"
}

// grantedScope is a human-readable resource scope a request would grant against.
type grantedScope struct {
	Kind string // e.g. "Repository", "Pull request"
	ID   string // e.g. "clems4ever/go-gnupg"
}

// approvalView is the data passed to the approval page template.
type approvalView struct {
	ID            string
	OperationType string
	Summary       string          // one-line "what will happen"
	Detail        string          // the exact content to be submitted (may be empty)
	Granted       []grantedAction // plain-English actions this would grant
	Scopes        []grantedScope  // resources the grant is limited to
	Policies      []string        // raw Cedar, shown collapsed
	Status        api.OperationStatus
	Decided       bool
	TTLOptions    []struct {
		Label string
		Value string
	}
}

// actionRE and scopeRE extract the action names and resource scopes from a
// generated Cedar policy so they can be presented in plain language.
var (
	actionRE = regexp.MustCompile(`GitHub::Action::"([^"]+)"`)
	scopeRE  = regexp.MustCompile(`resource (?:==|in) (GitHub::\w+)::"([^"]+)"`)
)

// splitDescription separates an operation's one-line summary from the exact
// content block that follows it (operations format that content after a colon
// and a blank line). When there is no content block, detail is empty.
//
// @arg desc The operation's full Describe() string.
// @return string The one-line summary (trailing colon trimmed).
// @return string The content block, or "" when the description has none.
//
// @testcase TestSplitDescription separates the summary from the content body.
func splitDescription(desc string) (summary, detail string) {
	if head, body, found := strings.Cut(desc, "\n\n"); found {
		return strings.TrimRight(strings.TrimSpace(head), ":"), strings.TrimSpace(body)
	}
	return strings.TrimSpace(desc), ""
}

// grantedActionsFromPolicies resolves the action names referenced in the proposed
// Cedar policies into plain-English entries via the catalog.
//
// @arg policies The proposed Cedar policy texts.
// @return []grantedAction One entry per distinct action, in first-seen order.
//
// @testcase TestGrantedActionsFromPolicies resolves a known action to its title and kind.
func grantedActionsFromPolicies(policies []string) []grantedAction {
	cat := catalog.Build()
	byName := map[string]catalog.Action{}
	for _, a := range cat.Actions {
		byName[a.Name] = a
	}
	groupByName := map[string]catalog.Group{}
	for _, g := range cat.Groups {
		groupByName[g.Name] = g
	}

	seen := map[string]bool{}
	var out []grantedAction
	for _, p := range policies {
		for _, m := range actionRE.FindAllStringSubmatch(p, -1) {
			name := m[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			if a, ok := byName[name]; ok {
				kind := "read"
				if a.Mutating {
					kind = "write"
				}
				out = append(out, grantedAction{Name: name, Title: a.Title, Description: a.Description, Kind: kind})
			} else if g, ok := groupByName[name]; ok {
				out = append(out, grantedAction{Name: name, Title: g.Title, Description: g.Description, Kind: "group"})
			} else {
				out = append(out, grantedAction{Name: name, Title: name, Kind: "group"})
			}
		}
	}
	return out
}

// scopesFromPolicies resolves the resource scopes referenced in the proposed Cedar
// policies into human-readable entries.
//
// @arg policies The proposed Cedar policy texts.
// @return []grantedScope One entry per distinct resource scope, in first-seen order.
//
// @testcase TestScopesFromPolicies resolves a repo scope to its friendly label.
func scopesFromPolicies(policies []string) []grantedScope {
	friendly := map[string]string{
		"GitHub::Org":          "Owner / org",
		"GitHub::Repo":         "Repository",
		"GitHub::Issue":        "Issue",
		"GitHub::IssueComment": "Issue comment",
		"GitHub::PullRequest":  "Pull request",
		"GitHub::Branch":       "Branch",
	}
	seen := map[string]bool{}
	var out []grantedScope
	for _, p := range policies {
		for _, m := range scopeRE.FindAllStringSubmatch(p, -1) {
			kind, id := m[1], m[2]
			key := kind + id
			if seen[key] {
				continue
			}
			seen[key] = true
			label := friendly[kind]
			if label == "" {
				label = kind
			}
			out = append(out, grantedScope{Kind: label, ID: id})
		}
	}
	return out
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
		http.Error(w, "grant request not found", http.StatusNotFound)
		return
	}
	summary, detail := splitDescription(dr.Description)
	view := approvalView{
		ID:            dr.ID,
		OperationType: dr.OperationType,
		Summary:       summary,
		Detail:        detail,
		Granted:       grantedActionsFromPolicies(dr.ProposedPolicies),
		Scopes:        scopesFromPolicies(dr.ProposedPolicies),
		Policies:      dr.ProposedPolicies,
		Status:        dr.Status,
		Decided:       dr.Status != api.StatusPending,
		TTLOptions:    ttlOptions,
	}
	_ = s.render(w, r, "approve", view)
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
			message = "The grant request has been approved. You can return to your terminal."
		}
	case "reject":
		if _, err = s.store.Reject(id); err == nil {
			status = api.StatusRejected
			message = "The grant request has been rejected."
		}
	default:
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	_ = s.render(w, r, "result", struct {
		Status  api.OperationStatus
		Message string
	}{Status: status, Message: message})
}
