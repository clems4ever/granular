package server

import (
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/api"
)

// TestBuildPresentationResolvesTitles resolves action titles and renders the scope.
func TestBuildPresentationResolvesTitles(t *testing.T) {
	caps := []api.Capability{{
		Actions:  []string{"issues.read"},
		Resource: api.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "octocat", "name": "Hello-World"}},
	}}

	p := buildPresentation("", caps)
	if len(p.Permissions) != 1 || !strings.Contains(p.Permissions[0], "issues") {
		t.Fatalf("permissions = %v, want a resolved issues title", p.Permissions)
	}
	if len(p.Scopes) != 1 || p.Scopes[0] != "octocat/Hello-World" {
		t.Fatalf("scopes = %v, want octocat/Hello-World", p.Scopes)
	}
	if p.Title != "Access request" {
		t.Fatalf("title = %q", p.Title)
	}

	// A wildcard name widens the scope; a reason overrides the summary.
	wide := []api.Capability{{Actions: []string{"issues.read"}, Resource: api.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "octocat", "name": "*"}}}}
	if got := buildPresentation("because", wide); got.Summary != "because" || !strings.Contains(got.Scopes[0], "all repositories under octocat") {
		t.Fatalf("unexpected wide presentation: %+v", got)
	}
}
