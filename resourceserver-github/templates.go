package resourceservergithub

import "github.com/clems4ever/granular/resourceserver"

// Shared template parameters reused across the GitHub templates.
var (
	tpOwner = resourceserver.TemplateParam{Name: "owner", Description: "repository owner/org login", Required: true, Field: "owner"}
	tpName  = resourceserver.TemplateParam{Name: "name", Description: "repository name ('*' or omit for the whole org)", Field: "name"}
)

// templates returns the GitHub permission templates: resource server-authored, parameterized
// shapes a client can instantiate (instead of assembling a raw capability) for clearer
// consent. Scope params (owner/name) fill the github.repo selector; condition params and
// fixed values become Cedar attribute conditions the consent screen describes in plain
// language.
//
// @return []resourceserver.Template The GitHub templates offered to clients.
//
// @testcase TestTemplatesExpand expands every template with sample bindings.
func templates() []resourceserver.Template {
	return []resourceserver.Template{
		{
			Name: "read-repo", Title: "Read a repository",
			Description: "Read access (clone, issues, pull requests, comments) to a repository.",
			Summary:     "Read everything in {owner}/{name}",
			Actions:     []string{"read"},
			Scope:       "github.repo",
			Params:      []resourceserver.TemplateParam{tpOwner, tpName},
		},
		{
			Name: "comment-on-open-issues", Title: "Comment on open issues",
			Description: "Comment on the open issues of a repository, optionally only those carrying a label.",
			Summary:     "Comment on open issues in {owner}/{name} labeled {label}",
			Actions:     []string{"issue.comment"},
			Scope:       "github.repo",
			Params: []resourceserver.TemplateParam{
				tpOwner, tpName,
				{Name: "state", Attr: "state", Op: "eq", Fixed: "open"},
				{Name: "label", Description: "only issues carrying this label", Attr: "labels", Op: "contains"},
			},
		},
		{
			Name: "triage-issues", Title: "Triage issues",
			Description: "Close and reopen issues in a repository.",
			Summary:     "Close and reopen issues in {owner}/{name}",
			Actions:     []string{"issues.triage"},
			Scope:       "github.repo",
			Params:      []resourceserver.TemplateParam{tpOwner, tpName},
		},
	}
}
