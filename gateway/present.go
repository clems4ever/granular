package gateway

import (
	"fmt"

	"github.com/clems4ever/granular/internal/proposal"
)

// buildPresentation authors the human-readable description for a capability bundle: one
// GrantDetail per capability (its actions and its resolved resource scope), index-aligned
// with the policies. The gateway — not the client — produces this, so the consent screen
// shows trustworthy descriptions the client cannot have crafted.
//
// @arg s The schema supplying the scope labeler.
// @arg reason The client's stated reason, used as the summary when present.
// @arg caps The requested capabilities.
// @return proposal.Presentation The presentation the AS displays verbatim.
//
// @testcase TestPresentation renders one grant detail per capability with its scope.
func buildPresentation(s Schema, reason string, caps []Capability) proposal.Presentation {
	grants := make([]proposal.GrantDetail, 0, len(caps))
	for _, c := range caps {
		grants = append(grants, proposal.GrantDetail{
			Actions:  c.Actions,
			Resource: scopeLabel(s, c.Resource),
		})
	}
	summary := fmt.Sprintf("Grant %d permission set(s).", len(grants))
	if reason != "" {
		summary = reason
	}
	return proposal.Presentation{Title: "Access request", Summary: summary, Grants: grants}
}

// scopeLabel renders a resource selector in plain language using the schema's ScopeFunc,
// falling back to a generic type+match rendering when no resolver is set or it errors.
//
// @arg s The schema supplying the scope labeler.
// @arg sel The resource selector.
// @return string A human-readable scope label.
//
// @testcase TestPresentation renders a repo scope label.
func scopeLabel(s Schema, sel ResourceSelector) string {
	if s.Scope != nil {
		if _, _, label, err := s.Scope(sel); err == nil && label != "" {
			return label
		}
	}
	return fmt.Sprintf("%s %v", sel.Type, sel.Match)
}
