package resourceserver

import (
	"fmt"

	"github.com/clems4ever/granular/internal/proposal"
)

// buildPresentation authors the human-readable description for a capability bundle: one
// GrantDetail per capability (its actions rendered as friendly labels and its resolved
// resource scope), index-aligned with the policies. The resource server — not the client —
// produces this, so the consent screen shows trustworthy descriptions the client cannot
// have crafted.
//
// @arg s The schema supplying the action labels and scope labeler.
// @arg reason The client's stated reason, used as the summary when present.
// @arg caps The requested capabilities.
// @return proposal.Presentation The presentation the AS displays verbatim.
//
// @testcase TestPresentation renders one grant detail per capability with friendly labels.
func buildPresentation(s Schema, reason string, caps []Capability) proposal.Presentation {
	grants := make([]proposal.GrantDetail, 0, len(caps))
	for _, c := range caps {
		typeName, label := scopeResource(s, c.Resource)
		grants = append(grants, proposal.GrantDetail{
			Actions:      actionLabels(s, c.Actions),
			ResourceType: typeName,
			Resource:     label,
		})
	}
	summary := fmt.Sprintf("Grant %d permission set(s).", len(grants))
	if reason != "" {
		summary = reason
	}
	return proposal.Presentation{Title: "Access request", Summary: summary, Grants: grants}
}

// actionLabels renders action or group names as the plain-language phrases shown on the
// consent screen, preferring each one's schema description, then its title, then the raw
// name as a last resort.
//
// @arg s The schema supplying action and group titles and descriptions.
// @arg names The action or group names to label.
// @return []string One human-readable label per name.
//
// @testcase TestPresentation labels a group by its description rather than its raw name.
func actionLabels(s Schema, names []string) []string {
	title := map[string]string{}
	desc := map[string]string{}
	for _, a := range s.Actions {
		title[a.Name], desc[a.Name] = a.Title, a.Description
	}
	for _, g := range s.Groups {
		title[g.Name], desc[g.Name] = g.Title, g.Description
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		switch {
		case desc[n] != "":
			out = append(out, desc[n])
		case title[n] != "":
			out = append(out, title[n])
		default:
			out = append(out, n)
		}
	}
	return out
}

// scopeResource resolves a resource selector to its human type name and its plain-language
// value using the schema's ScopeFunc, falling back to a generic rendering when no resolver
// is set or it errors. The type name lets the consent screen label an otherwise-ambiguous
// value (e.g. "Repository" for "clems4ever/granular").
//
// @arg s The schema supplying the scope resolver and resource titles.
// @arg sel The resource selector.
// @return string The human type name, e.g. "Repository".
// @return string The human-readable scope value, e.g. "clems4ever/granular".
//
// @testcase TestPresentation labels a repo scope with its type name and value.
func scopeResource(s Schema, sel ResourceSelector) (typeName, label string) {
	if s.Scope != nil {
		if entity, _, lbl, err := s.Scope(sel); err == nil {
			if lbl == "" {
				lbl = fmt.Sprintf("%v", sel.Match)
			}
			return resourceTypeName(s, entity), lbl
		}
	}
	return resourceTypeName(s, sel.Type), fmt.Sprintf("%s %v", sel.Type, sel.Match)
}

// resourceTypeName returns the human name for a resource kind, matching key against either
// a resource's Cedar entity type or its schema name, and falling back to the key itself.
//
// @arg s The schema holding the resource types.
// @arg key A Cedar entity type (e.g. "GitHub::Repo") or a resource name (e.g. "github.repo").
// @return string The resource's human title, or the key when unknown.
//
// @testcase TestPresentation resolves a resource type's human name.
func resourceTypeName(s Schema, key string) string {
	for _, r := range s.Resources {
		if r.Entity == key || r.Name == key {
			if r.Title != "" {
				return r.Title
			}
			return r.Name
		}
	}
	return key
}
