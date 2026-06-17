// Package catalog describes the GitHub capability model — the typed resource
// hierarchy, the verb lattice (action groups), and the concrete operations with
// their CLI commands. It is the single source the GitHub gateway builds its
// published permission schema from, so an agent or a human can see what can be
// requested and how grants are scoped.
package catalog

import "github.com/clems4ever/granular/gateway"

// MatchField is a typed attribute a resource can be matched on in a grant.
type MatchField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// ResourceType is a node in the typed resource hierarchy. Entity is its Cedar
// entity-type name, binding the catalog to the policy engine.
type ResourceType struct {
	Name        string       `json:"name"`
	Title       string       `json:"title"`
	Entity      string       `json:"entity"`
	Parent      string       `json:"parent,omitempty"`
	Description string       `json:"description"`
	Match       []MatchField `json:"match"`
}

// Group is a verb-lattice node: a roll-up that nests other groups (via Parents)
// and ultimately the concrete actions.
type Group struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Parents     []string `json:"parents,omitempty"`
}

// Action is a concrete operation: what it acts on, which groups it rolls up into,
// the CLI command that triggers it, whether it mutates, and how the resulting
// grant is scoped.
type Action struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Resource    string   `json:"resource"`
	Groups      []string `json:"groups"`
	CLI         string   `json:"cli,omitempty"`
	Mutating    bool     `json:"mutating"`
	Scope       string   `json:"scope"`
	Description string   `json:"description"`
}

// Catalog is the full capability manifest.
type Catalog struct {
	Resources      []ResourceType       `json:"resources"`
	Groups         []Group              `json:"groups"`
	Actions        []Action             `json:"actions"`
	RequestExample gateway.GrantRequest `json:"request_example"`
}

// Build returns the capability catalog for the GitHub operations the CLI exposes
// today (plus a few planned ones, marked by an empty CLI command).
//
// @return Catalog The assembled capability manifest.
//
// @testcase TestCatalogIsConsistent builds the catalog and validates its references.
func Build() Catalog {
	return Catalog{
		Resources: []ResourceType{
			{Name: "github.org", Title: "Organization / owner", Entity: "GitHub::Org", Description: "A GitHub account that owns repositories.",
				Match: []MatchField{{"login", "string (glob)", "owner login, e.g. clems4ever or clems4ever-*"}}},
			{Name: "github.repo", Title: "Repository", Entity: "GitHub::Repo", Parent: "github.org", Description: "A git repository.",
				Match: []MatchField{{"owner", "string (glob)", "owner login"}, {"name", "string (glob)", "repository name"}}},
			{Name: "github.issue", Title: "Issue", Entity: "GitHub::Issue", Parent: "github.repo", Description: "An issue in a repository.",
				Match: []MatchField{{"number", "int", "issue number"}, {"state", "enum(open,closed)", "issue state"}, {"labels", "set<string>", "issue labels"}, {"author", "string", "issue author"}}},
			{Name: "github.comment", Title: "Issue comment", Entity: "GitHub::IssueComment", Parent: "github.issue", Description: "A comment on an issue.",
				Match: []MatchField{{"id", "int", "comment id"}}},
			{Name: "github.pull", Title: "Pull request", Entity: "GitHub::PullRequest", Parent: "github.repo", Description: "A pull request in a repository.",
				Match: []MatchField{{"number", "int", "PR number"}, {"state", "enum(open,closed)", "PR state"}}},
			{Name: "github.branch", Title: "Branch", Entity: "GitHub::Branch", Parent: "github.repo", Description: "A git branch (push target).",
				Match: []MatchField{{"name", "string (glob)", "branch name, e.g. feature/*"}}},
		},
		Groups: []Group{
			{Name: "read", Title: "read", Description: "Everything readable: list/view, clone, read comments."},
			{Name: "write", Title: "write", Description: "Everything that creates or changes content."},
			{Name: "triage", Title: "triage", Description: "Status changes (close/reopen)."},
			{Name: "issues.read", Title: "issues:read", Parents: []string{"read"}, Description: "List and view issues."},
			{Name: "issues.write", Title: "issues:write", Parents: []string{"write"}, Description: "Create/comment/edit issues."},
			{Name: "issues.triage", Title: "issues:triage", Parents: []string{"triage"}, Description: "Close/reopen issues."},
			{Name: "pulls.read", Title: "pulls:read", Parents: []string{"read"}, Description: "List, view and diff pull requests."},
			{Name: "pulls.write", Title: "pulls:write", Parents: []string{"write"}, Description: "Create/comment/review/edit/merge pull requests."},
			{Name: "pulls.triage", Title: "pulls:triage", Parents: []string{"triage"}, Description: "Close/reopen pull requests."},
		},
		Actions: []Action{
			{"repo.clone", "Clone repository", "github.repo", []string{"read"}, "granular github clone <repo> <dest>", false, "per repository", "Clone a repository locally through the git proxy."},
			{"repo.push", "Push to repository", "github.repo", []string{"write"}, "granular github push <repo> <dir>", true, "per repository (through the git proxy)", "Push commits to a repository through the git proxy."},
			{"issue.list", "List issues", "github.repo", []string{"issues.read"}, "granular github issue list <repo>", false, "per repository + state", "List a repository's issues."},
			{"issue.view", "View issue", "github.issue", []string{"issues.read"}, "granular github issue view <repo> <number>", false, "per issue", "View a single issue's details."},
			{"comment.read", "Read issue comments", "github.comment", []string{"read"}, "granular github issue view <repo> <number> --comments", false, "per issue (separate from view)", "Read an issue's comments."},
			{"issue.create", "Create issue", "github.repo", []string{"issues.write"}, "granular github issue create <repo> --title …", true, "per repository + exact content", "Open a new issue."},
			{"issue.comment", "Comment on issue", "github.issue", []string{"issues.write"}, "granular github issue comment <repo> <number> --body …", true, "per issue + exact content", "Post a comment on an issue."},
			{"issue.edit", "Edit issue", "github.issue", []string{"issues.write"}, "granular github issue edit <repo> <number> …", true, "per issue + exact change set", "Edit an issue's fields."},
			{"issue.close", "Close issue", "github.issue", []string{"issues.triage"}, "granular github issue close <repo> <number>", true, "per issue (+reason)", "Close an issue."},
			{"issue.reopen", "Reopen issue", "github.issue", []string{"issues.triage"}, "granular github issue reopen <repo> <number>", true, "per issue", "Reopen an issue."},
			{"pull.list", "List pull requests", "github.repo", []string{"pulls.read"}, "granular github pr list <repo>", false, "per repository + state", "List a repository's pull requests."},
			{"pull.view", "View pull request", "github.pull", []string{"pulls.read"}, "granular github pr view <repo> <number>", false, "per pull request", "View a single pull request's details."},
			{"pull.diff", "View pull request diff", "github.pull", []string{"pulls.read"}, "granular github pr diff <repo> <number>", false, "per pull request", "View a pull request's unified diff."},
			{"pull.create", "Create pull request", "github.repo", []string{"pulls.write"}, "granular github pr create <repo> …", true, "per repository + exact content", "Open a pull request."},
			{"pull.comment", "Comment on pull request", "github.pull", []string{"pulls.write"}, "granular github pr comment <repo> <number> --body …", true, "per pull request + exact content", "Post a comment on a pull request."},
			{"pull.review", "Review pull request", "github.pull", []string{"pulls.write"}, "granular github pr review <repo> <number> …", true, "per pull request + exact verdict/content", "Approve, request changes on, or comment-review a pull request."},
			{"pull.edit", "Edit pull request", "github.pull", []string{"pulls.write"}, "granular github pr edit <repo> <number> …", true, "per pull request + exact change set", "Edit a pull request's title, body or base branch."},
			{"pull.merge", "Merge pull request", "github.pull", []string{"pulls.write"}, "granular github pr merge <repo> <number>", true, "per pull request (+method/sha)", "Merge a pull request."},
			{"pull.close", "Close pull request", "github.pull", []string{"pulls.triage"}, "granular github pr close <repo> <number>", true, "per pull request", "Close a pull request."},
			{"pull.reopen", "Reopen pull request", "github.pull", []string{"pulls.triage"}, "granular github pr reopen <repo> <number>", true, "per pull request", "Reopen a pull request."},
		},
		RequestExample: gateway.GrantRequest{
			Reason: "Work on the granular project: clone, read issues + comments, read PRs.",
			Capabilities: []gateway.Capability{{
				Actions: []string{"repo.clone", "issues.read", "comment.read", "pulls.read"},
				Resource: gateway.ResourceSelector{
					Type:  "github.repo",
					Match: map[string]string{"owner": "clems4ever", "name": "granular"},
				},
			}},
		},
	}
}
