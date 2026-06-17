package authz

import "testing"

// TestVerifyWorldRoundTrips checks that the generic requests and entity world derived
// from a requirement name the right principal/action/resource and include the action
// lattice and the resource's parent chain.
func TestVerifyWorldRoundTrips(t *testing.T) {
	reqs := []Requirement{{Action: "issue.view", Resource: IssueRef("octocat/Hello-World", 1)}}

	rq := VerifyRequests(reqs)
	if len(rq) != 1 {
		t.Fatalf("requests = %d, want 1", len(rq))
	}
	if rq[0].Action.ID != "issue.view" || rq[0].Resource.Type != "GitHub::Issue" || rq[0].Principal.Type != "GitHub::Agent" {
		t.Fatalf("unexpected request: %+v", rq[0])
	}

	present := map[string]bool{}
	for _, e := range VerifyWorld(reqs) {
		present[e.Type+"::"+e.ID] = true
	}
	for _, want := range []string{
		"GitHub::Agent::agent",
		"GitHub::Action::issue.view",
		"GitHub::Issue::octocat/Hello-World#1",
		"GitHub::Repo::octocat/Hello-World",
		"GitHub::Org::octocat",
	} {
		if !present[want] {
			t.Fatalf("entity world missing %q", want)
		}
	}
}
