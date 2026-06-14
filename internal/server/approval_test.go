package server

import "testing"

func TestSplitDescription(t *testing.T) {
	summary, detail := splitDescription("Open a pull request titled \"x\" (a → b):\n\nThe body text.\n\nmore")
	if summary != "Open a pull request titled \"x\" (a → b)" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if detail != "The body text.\n\nmore" {
		t.Fatalf("unexpected detail: %q", detail)
	}

	// No content block: detail is empty, summary is the whole thing.
	s2, d2 := splitDescription("Clone GitHub repository o/n through the granular proxy")
	if s2 != "Clone GitHub repository o/n through the granular proxy" || d2 != "" {
		t.Fatalf("unexpected split: %q / %q", s2, d2)
	}
}

func TestGrantedActionsFromPolicies(t *testing.T) {
	policies := []string{
		`permit ( principal == GitHub::Agent::"agent", action == GitHub::Action::"pull.create", resource == GitHub::Repo::"o/n" );`,
	}
	got := grantedActionsFromPolicies(policies)
	if len(got) != 1 {
		t.Fatalf("want 1 action, got %d: %+v", len(got), got)
	}
	if got[0].Name != "pull.create" || got[0].Kind != "write" || got[0].Title == "" {
		t.Fatalf("unexpected granted action: %+v", got[0])
	}

	// A read action resolves to kind "read"; duplicates collapse.
	read := grantedActionsFromPolicies([]string{
		`action == GitHub::Action::"pull.list"`,
		`action == GitHub::Action::"pull.list"`,
	})
	if len(read) != 1 || read[0].Kind != "read" {
		t.Fatalf("expected one read action, got %+v", read)
	}
}

func TestScopesFromPolicies(t *testing.T) {
	scopes := scopesFromPolicies([]string{
		`permit ( principal == GitHub::Agent::"agent", action == GitHub::Action::"pull.create", resource == GitHub::Repo::"clems4ever/go-gnupg" );`,
	})
	if len(scopes) != 1 || scopes[0].Kind != "Repository" || scopes[0].ID != "clems4ever/go-gnupg" {
		t.Fatalf("unexpected scopes: %+v", scopes)
	}

	// `resource in` (capability-style) scopes resolve too.
	org := scopesFromPolicies([]string{`resource in GitHub::Org::"clems4ever"`})
	if len(org) != 1 || org[0].Kind != "Owner / org" || org[0].ID != "clems4ever" {
		t.Fatalf("unexpected org scope: %+v", org)
	}
}
