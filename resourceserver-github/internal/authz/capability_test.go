package authz

import "testing"

// TestRefsBuildHierarchy checks the resource-reference constructors build the
// expected ids and parent chains (issue/pull parented to repo, repo to its org).
func TestRefsBuildHierarchy(t *testing.T) {
	repo := RepoRef("owner/name")
	if repo.Type != "github.repo" || repo.ID != "owner/name" {
		t.Fatalf("unexpected repo ref %+v", repo)
	}
	if repo.Parent == nil || repo.Parent.Type != "github.org" || repo.Parent.ID != "owner" {
		t.Fatalf("repo ref should be parented to its org: %+v", repo.Parent)
	}

	issue := IssueRef("owner/name", 7)
	if issue.Type != "github.issue" || issue.ID != "owner/name#7" {
		t.Fatalf("unexpected issue ref %+v", issue)
	}
	if issue.Parent == nil || issue.Parent.Type != "github.repo" || issue.Parent.ID != "owner/name" {
		t.Fatalf("issue ref should be parented to its repo: %+v", issue.Parent)
	}

	pull := PullRef("owner/name", 3)
	if pull.Type != "github.pull" || pull.ID != "owner/name#3" {
		t.Fatalf("unexpected pull ref %+v", pull)
	}
	if pull.Parent == nil || pull.Parent.ID != "owner/name" {
		t.Fatalf("pull ref should be parented to its repo: %+v", pull.Parent)
	}
}
