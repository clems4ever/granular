package proposal

import "testing"

// samplePresentation returns a presentation used across the tests.
func samplePresentation() Presentation {
	return Presentation{Title: "View issue", Summary: "View issue octocat/Hello-World#1", Grants: []GrantDetail{{Actions: []string{"issue.view"}, Resource: "octocat/Hello-World#1"}}}
}

// TestSignVerifyRoundTrip signs a request and verifies it with the same secret.
func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("s3cret")
	s := Sign(secret, "github-gateway", samplePresentation(), []string{"permit(principal,action,resource);"})
	if !s.Verify(secret) {
		t.Fatal("freshly signed request failed to verify")
	}
}

// TestVerifyRejectsTamperedPresentation fails when the presentation is altered after
// signing (a relaying client swapping benign text onto broad policies).
func TestVerifyRejectsTamperedPresentation(t *testing.T) {
	secret := []byte("s3cret")
	s := Sign(secret, "gw", samplePresentation(), []string{"permit(x);"})
	s.Presentation.Summary = "something harmless"
	if s.Verify(secret) {
		t.Fatal("tampered presentation verified; want failure")
	}
}

// TestVerifyRejectsTamperedPolicies fails when the policies are altered after signing.
func TestVerifyRejectsTamperedPolicies(t *testing.T) {
	secret := []byte("s3cret")
	s := Sign(secret, "gw", samplePresentation(), []string{"permit(x);"})
	s.Policies = []string{"permit(everything);"}
	if s.Verify(secret) {
		t.Fatal("tampered policies verified; want failure")
	}
}

// TestVerifyRejectsWrongSecret fails when verifying under a different secret.
func TestVerifyRejectsWrongSecret(t *testing.T) {
	s := Sign([]byte("s3cret"), "gw", samplePresentation(), []string{"permit(x);"})
	if s.Verify([]byte("other")) {
		t.Fatal("verified under wrong secret; want failure")
	}
}
