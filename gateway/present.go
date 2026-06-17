package gateway

import (
	"fmt"

	"github.com/clems4ever/granular/internal/proposal"
)

// buildPresentation authors the human-readable description for a capability bundle,
// resolving action names to their schema titles and descriptions and rendering each
// resource scope with the schema's ScopeFunc. The gateway — not the client — produces
// this text, so the consent screen shows trustworthy descriptions the client cannot
// have crafted.
//
// @arg s The schema supplying action titles/descriptions and the scope labeler.
// @arg reason The client's stated reason, used as the summary when present.
// @arg caps The requested capabilities.
// @return proposal.Presentation The presentation the AS displays verbatim.
//
// @testcase TestPresentation resolves action titles and renders scope labels.
func buildPresentation(s Schema, reason string, caps []Capability) proposal.Presentation {
	title := map[string]string{}
	desc := map[string]string{}
	for _, a := range s.Actions {
		title[a.Name], desc[a.Name] = a.Title, a.Description
	}
	for _, g := range s.Groups {
		title[g.Name], desc[g.Name] = g.Title, g.Description
	}

	seen := map[string]bool{}
	var permissions, scopes []string
	for _, c := range caps {
		for _, act := range c.Actions {
			if seen[act] {
				continue
			}
			seen[act] = true
			label := title[act]
			if label == "" {
				label = act
			}
			if d := desc[act]; d != "" {
				label += " — " + d
			}
			permissions = append(permissions, label)
		}
		scopes = append(scopes, scopeLabel(s, c.Resource))
	}

	summary := fmt.Sprintf("Grant %d permission(s) across %d scope(s).", len(permissions), len(scopes))
	if reason != "" {
		summary = reason
	}
	return proposal.Presentation{
		Title:       "Access request",
		Summary:     summary,
		Permissions: permissions,
		Scopes:      scopes,
	}
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
