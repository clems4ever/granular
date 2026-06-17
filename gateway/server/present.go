package server

import (
	"fmt"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/catalog"
	"github.com/clems4ever/granular/internal/proposal"
)

// buildPresentation authors the human-readable description for a capability bundle,
// resolving action names to their catalog titles and rendering each resource scope in
// plain language. The gateway — not the client — produces this text, so the consent
// screen shows trustworthy descriptions the client cannot have crafted.
//
// @arg reason The client's stated reason, shown in the summary when present.
// @arg caps The requested capabilities.
// @return proposal.Presentation The presentation displayed by the AS verbatim.
//
// @testcase TestBuildPresentationResolvesTitles renders titles and scopes.
func buildPresentation(reason string, caps []api.Capability) proposal.Presentation {
	cat := catalog.Build()
	title := map[string]string{}
	desc := map[string]string{}
	for _, a := range cat.Actions {
		title[a.Name], desc[a.Name] = a.Title, a.Description
	}
	for _, g := range cat.Groups {
		title[g.Name], desc[g.Name] = g.Title, g.Description
	}

	seenPerm := map[string]bool{}
	var permissions, scopes []string
	for _, c := range caps {
		for _, act := range c.Actions {
			if seenPerm[act] {
				continue
			}
			seenPerm[act] = true
			label := title[act]
			if label == "" {
				label = act
			}
			if d := desc[act]; d != "" {
				label += " — " + d
			}
			permissions = append(permissions, label)
		}
		scopes = append(scopes, scopeString(c.Resource))
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

// scopeString renders a resource selector in plain language.
//
// @arg sel The resource selector.
// @return string A human-readable scope, e.g. "owner/name" or "all repositories under owner".
//
// @testcase TestBuildPresentationResolvesTitles renders a repo scope.
func scopeString(sel api.ResourceSelector) string {
	if sel.Type == "github.repo" {
		owner := sel.Match["owner"]
		if name := sel.Match["name"]; name != "" && name != "*" {
			return owner + "/" + name
		}
		return "all repositories under " + owner
	}
	return fmt.Sprintf("%s %v", sel.Type, sel.Match)
}
