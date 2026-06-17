package gatewaygithub

import "github.com/clems4ever/granular/gateway"

// Shared parameter definitions reused across the GitHub operations.
var (
	pRepo   = gateway.Param{Name: "repo", Type: "string", Required: true, Description: "repository, owner/name"}
	pNumber = gateway.Param{Name: "number", Type: "int", Required: true, Description: "item number"}
	pState  = gateway.Param{Name: "state", Type: "enum(open,closed,all)", Required: false, Description: "filter by state (default open)"}
	pLimit  = gateway.Param{Name: "limit", Type: "int", Required: false, Description: "max results (default 30)"}
)

// operationSpecs describes every executable GitHub operation: the type id a client
// submits to `op`, the parameters it accepts, whether it mutates, and the action and
// resource type a grant must authorize. An agent reads these to perform work, having
// requested the matching action via a grant request.
//
// @return []gateway.OperationSpec The signatures of all GitHub operations.
//
// @testcase TestOperationSpecsCoverRegistry has one spec per registered operation.
func operationSpecs() []gateway.OperationSpec {
	return []gateway.OperationSpec{
		{Type: "github.clone", Title: "Clone repository", Action: "repo.clone", Resource: "github.repo", Mutating: false,
			Params: []gateway.Param{pRepo}, Description: "Clone a repository through the git proxy."},
		{Type: "github.push", Title: "Push to repository", Action: "repo.push", Resource: "github.repo", Mutating: true,
			Params: []gateway.Param{pRepo}, Description: "Push commits to a repository through the git proxy."},

		{Type: "github.issue.list", Title: "List issues", Action: "issue.list", Resource: "github.repo", Mutating: false,
			Params: []gateway.Param{pRepo, pState, pLimit}, Description: "List a repository's issues."},
		{Type: "github.issue.view", Title: "View issue", Action: "issue.view", Resource: "github.issue", Mutating: false,
			Params: []gateway.Param{pRepo, pNumber, {Name: "comments", Type: "bool", Description: "include comments"}}, Description: "View a single issue."},
		{Type: "github.issue.create", Title: "Create issue", Action: "issue.create", Resource: "github.repo", Mutating: true,
			Params: []gateway.Param{pRepo, {Name: "title", Type: "string", Required: true, Description: "issue title"}, {Name: "body", Type: "string", Description: "issue body"}, {Name: "labels", Type: "list<string>", Description: "labels to set"}, {Name: "assignees", Type: "list<string>", Description: "users to assign"}}, Description: "Open a new issue."},
		{Type: "github.issue.comment", Title: "Comment on issue", Action: "issue.comment", Resource: "github.issue", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber, {Name: "body", Type: "string", Required: true, Description: "comment text"}}, Description: "Post a comment on an issue."},
		{Type: "github.issue.edit", Title: "Edit issue", Action: "issue.edit", Resource: "github.issue", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber, {Name: "title", Type: "string", Description: "new title"}, {Name: "body", Type: "string", Description: "new body"}, {Name: "add_labels", Type: "list<string>", Description: "labels to add"}, {Name: "remove_labels", Type: "list<string>", Description: "labels to remove"}, {Name: "add_assignees", Type: "list<string>", Description: "assignees to add"}, {Name: "remove_assignees", Type: "list<string>", Description: "assignees to remove"}}, Description: "Edit an issue (at least one change required)."},
		{Type: "github.issue.close", Title: "Close issue", Action: "issue.close", Resource: "github.issue", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber, {Name: "reason", Type: "enum(completed,not planned)", Description: "close reason"}}, Description: "Close an issue."},
		{Type: "github.issue.reopen", Title: "Reopen issue", Action: "issue.reopen", Resource: "github.issue", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber}, Description: "Reopen an issue."},

		{Type: "github.pull.list", Title: "List pull requests", Action: "pull.list", Resource: "github.repo", Mutating: false,
			Params: []gateway.Param{pRepo, pState, pLimit}, Description: "List a repository's pull requests."},
		{Type: "github.pull.view", Title: "View pull request", Action: "pull.view", Resource: "github.pull", Mutating: false,
			Params: []gateway.Param{pRepo, pNumber, {Name: "comments", Type: "bool", Description: "include comments"}}, Description: "View a single pull request."},
		{Type: "github.pull.diff", Title: "View pull request diff", Action: "pull.diff", Resource: "github.pull", Mutating: false,
			Params: []gateway.Param{pRepo, pNumber}, Description: "View a pull request's unified diff."},
		{Type: "github.pull.create", Title: "Create pull request", Action: "pull.create", Resource: "github.repo", Mutating: true,
			Params: []gateway.Param{pRepo, {Name: "title", Type: "string", Required: true, Description: "PR title"}, {Name: "head", Type: "string", Required: true, Description: "source branch"}, {Name: "base", Type: "string", Required: true, Description: "target branch"}, {Name: "body", Type: "string", Description: "PR body"}, {Name: "draft", Type: "bool", Description: "open as draft"}}, Description: "Open a pull request."},
		{Type: "github.pull.comment", Title: "Comment on pull request", Action: "pull.comment", Resource: "github.pull", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber, {Name: "body", Type: "string", Required: true, Description: "comment text"}}, Description: "Post a comment on a pull request."},
		{Type: "github.pull.review", Title: "Review pull request", Action: "pull.review", Resource: "github.pull", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber, {Name: "event", Type: "enum(approve,request_changes,comment)", Required: true, Description: "review verdict"}, {Name: "body", Type: "string", Description: "review text (required unless approve)"}}, Description: "Approve, request changes on, or comment-review a pull request."},
		{Type: "github.pull.edit", Title: "Edit pull request", Action: "pull.edit", Resource: "github.pull", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber, {Name: "title", Type: "string", Description: "new title"}, {Name: "body", Type: "string", Description: "new body"}, {Name: "base", Type: "string", Description: "new base branch"}}, Description: "Edit a pull request (at least one change required)."},
		{Type: "github.pull.merge", Title: "Merge pull request", Action: "pull.merge", Resource: "github.pull", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber, {Name: "method", Type: "enum(merge,squash,rebase)", Description: "merge method (default merge)"}, {Name: "sha", Type: "string", Description: "expected head SHA"}}, Description: "Merge a pull request."},
		{Type: "github.pull.close", Title: "Close pull request", Action: "pull.close", Resource: "github.pull", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber}, Description: "Close a pull request."},
		{Type: "github.pull.reopen", Title: "Reopen pull request", Action: "pull.reopen", Resource: "github.pull", Mutating: true,
			Params: []gateway.Param{pRepo, pNumber}, Description: "Reopen a pull request."},
	}
}
