package authz

import "testing"

// engine builds an Engine from policy text or fails the test.
func engine(t *testing.T, policies string) *Engine {
	t.Helper()
	e, err := NewEngine(policies)
	if err != nil {
		t.Fatalf("parse policies: %v", err)
	}
	return e
}

// --- The verb lattice: list, view, and read ---------------------------------

// A grant of the global "read" group authorizes the concrete list and view
// actions the agent actually requests — you don't grant "list"+"view" by hand.
func TestReadRollupCoversListAndView(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"read",
		  resource in GitHub::Repo::"clems4ever/granular"
		);`)
	w := NewWorld()
	agent := w.Agent("session")
	repo := w.Repo("clems4ever/granular")
	issue := w.Issue("clems4ever/granular", 7, "open")

	// issue.list acts on the repo (the collection); issue.view on the issue.
	if !e.Allowed(w, agent, Action("issue.list"), repo) {
		t.Error("read should cover issue.list")
	}
	if !e.Allowed(w, agent, Action("issue.view"), issue) {
		t.Error("read should cover issue.view")
	}
}

// You can still grant a single concrete action when you want it narrow: granting
// only issue.view does NOT also grant issue.list.
func TestFineGrainedViewDoesNotCoverList(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action == GitHub::Action::"issue.view",
		  resource in GitHub::Repo::"clems4ever/granular"
		);`)
	w := NewWorld()
	agent := w.Agent("session")
	repo := w.Repo("clems4ever/granular")
	issue := w.Issue("clems4ever/granular", 7, "open")

	if !e.Allowed(w, agent, Action("issue.view"), issue) {
		t.Error("issue.view should be allowed")
	}
	if e.Allowed(w, agent, Action("issue.list"), repo) {
		t.Error("issue.list must NOT be covered by an issue.view-only grant")
	}
}

// The subtlety: list (collection op, resource = repo) and view (item op,
// resource = issue) target DIFFERENT resources. A read grant scoped to the item
// type (`resource is Issue`) silently misses list.
func TestItemScopedReadMissesList(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"issues.read",
		  resource in GitHub::Repo::"clems4ever/granular"
		) when { resource is GitHub::Issue };`)
	w := NewWorld()
	agent := w.Agent("session")
	repo := w.Repo("clems4ever/granular")
	issue := w.Issue("clems4ever/granular", 7, "open")

	if !e.Allowed(w, agent, Action("issue.view"), issue) {
		t.Error("view (resource=issue) should be allowed")
	}
	if e.Allowed(w, agent, Action("issue.list"), repo) {
		t.Error("list (resource=repo) is NOT an Issue, so item-scoped read misses it")
	}
}

// The robust pattern: scope read to the repo (no item-type filter) and let the
// action group (`issues.read`) decide the family. Then both the collection op
// (list) and the item op (view) are covered, while PRs are excluded by action.
func TestRepoScopedIssuesReadCoversBothAndExcludesPRs(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"issues.read",
		  resource in GitHub::Repo::"clems4ever/granular"
		);`)
	w := NewWorld()
	agent := w.Agent("session")
	repo := w.Repo("clems4ever/granular")
	issue := w.Issue("clems4ever/granular", 7, "open")
	pull := w.Pull("clems4ever/granular", 3, "open")

	if !e.Allowed(w, agent, Action("issue.list"), repo) {
		t.Error("issues.read should cover list")
	}
	if !e.Allowed(w, agent, Action("issue.view"), issue) {
		t.Error("issues.read should cover view")
	}
	if e.Allowed(w, agent, Action("pull.view"), pull) {
		t.Error("issues.read must NOT cover pull.view")
	}
}

// --- read on repo, rw on PRs ------------------------------------------------

func TestRwOnPullRequests(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"read",
		  resource == GitHub::Repo::"clems4ever/granular"
		);
		permit (
		  principal == GitHub::Agent::"session",
		  action in [GitHub::Action::"read", GitHub::Action::"write"],
		  resource in GitHub::Repo::"clems4ever/granular"
		) when { resource is GitHub::PullRequest };`)
	w := NewWorld()
	agent := w.Agent("session")
	repo := w.Repo("clems4ever/granular")
	pull := w.Pull("clems4ever/granular", 3, "open")
	issue := w.Issue("clems4ever/granular", 7, "open")

	if !e.Allowed(w, agent, Action("repo.clone"), repo) {
		t.Error("read on repo should cover clone")
	}
	if !e.Allowed(w, agent, Action("pull.view"), pull) {
		t.Error("rw on PR should cover pull.view")
	}
	if !e.Allowed(w, agent, Action("pull.create"), pull) {
		t.Error("rw on PR should cover pull.create")
	}
	if e.Allowed(w, agent, Action("issue.create"), issue) {
		t.Error("rw on PR must NOT grant writing issues")
	}
}

// --- matcher on the object: push only to feature/* branches -----------------

func TestPushOnlyToFeatureBranches(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action == GitHub::Action::"repo.push",
		  resource in GitHub::Repo::"clems4ever/granular"
		) when { resource is GitHub::Branch && resource.name like "feature/*" };`)
	w := NewWorld()
	agent := w.Agent("session")
	feature := w.Branch("clems4ever/granular", "feature/login")
	main := w.Branch("clems4ever/granular", "main")

	if !e.Allowed(w, agent, Action("repo.push"), feature) {
		t.Error("push to feature/* should be allowed")
	}
	if e.Allowed(w, agent, Action("repo.push"), main) {
		t.Error("push to main must be denied")
	}
}

// --- attribute matchers: read only open issues labelled bug -----------------

func TestReadOnlyOpenBugIssues(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"issues.read",
		  resource in GitHub::Repo::"clems4ever/granular"
		) when {
		  resource is GitHub::Issue && resource.state == "open" && resource.labels.contains("bug")
		};`)
	w := NewWorld()
	agent := w.Agent("session")
	openBug := w.Issue("clems4ever/granular", 1, "open", "bug", "p1")
	openOther := w.Issue("clems4ever/granular", 2, "open", "docs")
	closedBug := w.Issue("clems4ever/granular", 3, "closed", "bug")

	if !e.Allowed(w, agent, Action("issue.view"), openBug) {
		t.Error("open+bug should be allowed")
	}
	if e.Allowed(w, agent, Action("issue.view"), openOther) {
		t.Error("open without bug should be denied")
	}
	if e.Allowed(w, agent, Action("issue.view"), closedBug) {
		t.Error("closed should be denied")
	}
}

// --- globs as hierarchy: read issues across a whole org ---------------------

func TestOrgWideReadViaHierarchy(t *testing.T) {
	e := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"issues.read",
		  resource in GitHub::Org::"clems4ever"
		);`)
	w := NewWorld()
	agent := w.Agent("session")
	mine := w.Issue("clems4ever/granular", 7, "open")
	theirs := w.Issue("acme/widget", 7, "open")

	if !e.Allowed(w, agent, Action("issue.view"), mine) {
		t.Error("issue under clems4ever/* should be allowed")
	}
	if e.Allowed(w, agent, Action("issue.view"), theirs) {
		t.Error("issue under another org must be denied")
	}
}

// --- "+comments" is a separate capability -----------------------------------

func TestCommentsRequireSeparateCapability(t *testing.T) {
	w := NewWorld()
	agent := w.Agent("session")
	issue := w.Issue("clems4ever/granular", 7, "open")
	comment := w.Comment(issue, 99)

	// issues.read covers viewing the issue, but NOT reading its comments
	// (comment.read is deliberately outside the issues.read group).
	readIssues := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"issues.read",
		  resource in GitHub::Repo::"clems4ever/granular"
		);`)
	if !readIssues.Allowed(w, agent, Action("issue.view"), issue) {
		t.Error("issues.read should cover issue.view")
	}
	if readIssues.Allowed(w, agent, Action("comment.read"), comment) {
		t.Error("issues.read must NOT cover reading comments")
	}

	// Adding a comment-read grant unlocks it.
	withComments := engine(t, `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"issues.read",
		  resource in GitHub::Repo::"clems4ever/granular"
		);
		permit (
		  principal == GitHub::Agent::"session",
		  action == GitHub::Action::"comment.read",
		  resource in GitHub::Repo::"clems4ever/granular"
		);`)
	if !withComments.Allowed(w, agent, Action("comment.read"), comment) {
		t.Error("explicit comment.read grant should allow reading comments")
	}
}

// --- incremental, append-only grants ----------------------------------------

func TestIncrementalGrantAddsWrite(t *testing.T) {
	w := NewWorld()
	agent := w.Agent("session")
	issue := w.Issue("clems4ever/granular", 7, "open")

	readOnly := `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"read",
		  resource in GitHub::Repo::"clems4ever/granular"
		);`
	if engine(t, readOnly).Allowed(w, agent, Action("issue.comment"), issue) {
		t.Error("read-only must not allow commenting")
	}

	// Later the human appends one more permit; nothing else changes.
	withWrite := readOnly + `
		permit (
		  principal == GitHub::Agent::"session",
		  action in GitHub::Action::"issues.write",
		  resource in GitHub::Repo::"clems4ever/granular"
		);`
	if !engine(t, withWrite).Allowed(w, agent, Action("issue.comment"), issue) {
		t.Error("appended write grant should allow commenting")
	}
}
