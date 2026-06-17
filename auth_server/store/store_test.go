package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/clems4ever/granular/internal/proposal"
)

// openTemp opens a store backed by a temp-file database, cleaned up after the test.
//
// @arg t The test handle.
// @return *Store The store under test.
//
// @testcase TestPolicyLifecycle opens a store with this helper.
func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "as.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// item builds a minimal signed grant request carrying one opaque policy.
//
// @return proposal.SignedGrantRequest A signed item for gateway "gw".
//
// @testcase TestProposalApprovalAttachesGrants attaches this item.
func item() proposal.SignedGrantRequest {
	return proposal.Sign([]byte("s"), "gw", proposal.Presentation{Summary: "x"}, []string{"permit(principal, action, resource);"})
}

// TestPolicyLifecycle covers minting a policy token, checking existence, and destroy.
func TestPolicyLifecycle(t *testing.T) {
	s := openTemp(t)
	token, err := s.CreatePolicy()
	if err != nil || token == "" {
		t.Fatalf("CreatePolicy: %v %q", err, token)
	}
	if !s.PolicyExists(token) {
		t.Fatal("PolicyExists false for a freshly minted token")
	}
	if s.PolicyExists("nope") {
		t.Fatal("PolicyExists true for an unknown token")
	}
}

// TestProposalApprovalAttachesGrants approves a proposal and reads the attached policy.
func TestProposalApprovalAttachesGrants(t *testing.T) {
	s := openTemp(t)
	token, _ := s.CreatePolicy()
	p, err := s.CreateProposal(token, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	if got, err := s.GetProposal(p.ID); err != nil || got.ID != p.ID {
		t.Fatalf("GetProposal: %v", err)
	}
	if _, err := s.Approve(p.ID, time.Hour); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	grants, err := s.PolicyForToken(token)
	if err != nil || len(grants) != 1 {
		t.Fatalf("PolicyForToken: %v len=%d", err, len(grants))
	}
}

// TestApproveTwiceFails rejects approving an already-decided proposal.
func TestApproveTwiceFails(t *testing.T) {
	s := openTemp(t)
	token, _ := s.CreatePolicy()
	p, _ := s.CreateProposal(token, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	if _, err := s.Approve(p.ID, time.Hour); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if _, err := s.Approve(p.ID, time.Hour); err != ErrAlreadyDecided {
		t.Fatalf("second approve err = %v, want ErrAlreadyDecided", err)
	}
}

// TestRejectProposal marks a proposal rejected and attaches nothing.
func TestRejectProposal(t *testing.T) {
	s := openTemp(t)
	token, _ := s.CreatePolicy()
	p, _ := s.CreateProposal(token, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	if _, err := s.Reject(p.ID); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	grants, _ := s.PolicyForToken(token)
	if len(grants) != 0 {
		t.Fatalf("reject attached %d grants, want 0", len(grants))
	}
}

// TestGetMissingProposal returns ErrNotFound for an unknown id.
func TestGetMissingProposal(t *testing.T) {
	s := openTemp(t)
	if _, err := s.GetProposal("nope"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestExpiredGrantDropped omits (and deletes) a grant whose expiry has passed.
func TestExpiredGrantDropped(t *testing.T) {
	s := openTemp(t)
	token, _ := s.CreatePolicy()
	p, _ := s.CreateProposal(token, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	if _, err := s.Approve(p.ID, time.Millisecond); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// Advance the store clock past the grant's expiry.
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	grants, _ := s.PolicyForToken(token)
	if len(grants) != 0 {
		t.Fatalf("expired grant still active: %d", len(grants))
	}
}

// TestAllGrantsAndProposals lists active grants (omitting expired) and the full proposal
// history, newest first.
func TestAllGrantsAndProposals(t *testing.T) {
	s := openTemp(t)

	// An approved proposal with a long-lived grant.
	tokA, _ := s.CreatePolicy()
	pA, _ := s.CreateProposal(tokA, "a@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	if _, err := s.Approve(pA.ID, 24*time.Hour); err != nil {
		t.Fatalf("approve A: %v", err)
	}
	// An approved proposal whose grant expires almost immediately.
	tokB, _ := s.CreatePolicy()
	pB, _ := s.CreateProposal(tokB, "b@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	if _, err := s.Approve(pB.ID, time.Millisecond); err != nil {
		t.Fatalf("approve B: %v", err)
	}
	// A rejected proposal (history only, no grant).
	tokC, _ := s.CreatePolicy()
	pC, _ := s.CreateProposal(tokC, "c@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	if _, err := s.Reject(pC.ID); err != nil {
		t.Fatalf("reject C: %v", err)
	}

	// Advance the clock past B's expiry: only A's grant remains active.
	s.now = func() time.Time { return time.Now().Add(time.Hour) }

	grants, err := s.AllGrants()
	if err != nil || len(grants) != 1 || grants[0].Token != tokA {
		t.Fatalf("AllGrants = %+v err=%v (want one for A)", grants, err)
	}

	proposals, err := s.AllProposals()
	if err != nil || len(proposals) != 3 {
		t.Fatalf("AllProposals = %d err=%v (want 3)", len(proposals), err)
	}
	byStatus := map[Status]int{}
	for _, p := range proposals {
		byStatus[p.Status]++
	}
	if byStatus[StatusApproved] != 2 || byStatus[StatusRejected] != 1 {
		t.Fatalf("history statuses = %v", byStatus)
	}
}

// TestProposalExpires checks a pending proposal lapses: it cannot be approved once its
// window has passed (and no grant attaches), and the janitor revokes an untouched one.
func TestProposalExpires(t *testing.T) {
	s := openTemp(t)
	tok1, _ := s.CreatePolicy()
	p1, _ := s.CreateProposal(tok1, "a@example.com", []proposal.SignedGrantRequest{item()}, time.Minute)
	tok2, _ := s.CreatePolicy()
	p2, _ := s.CreateProposal(tok2, "b@example.com", []proposal.SignedGrantRequest{item()}, time.Minute)

	if p1.Expired(time.Now()) {
		t.Fatal("a fresh proposal reports expired")
	}

	// Advance the store clock past both proposals' expiry.
	s.now = func() time.Time { return time.Now().Add(2 * time.Minute) }

	// Approving a lapsed proposal is refused, marks it expired, and attaches no grant.
	if _, err := s.Approve(p1.ID, time.Hour); !errors.Is(err, ErrExpired) {
		t.Fatalf("Approve lapsed = %v, want ErrExpired", err)
	}
	if g, _ := s.GetProposal(p1.ID); g.Status != StatusExpired {
		t.Fatalf("p1 status = %q, want expired", g.Status)
	}
	if grants, _ := s.PolicyForToken(tok1); len(grants) != 0 {
		t.Fatalf("expired approval attached %d grant(s)", len(grants))
	}

	// The janitor revokes the untouched lapsed proposal.
	if n, err := s.PurgeExpired(); err != nil || n < 1 {
		t.Fatalf("PurgeExpired n=%d err=%v", n, err)
	}
	if g, _ := s.GetProposal(p2.ID); g.Status != StatusExpired {
		t.Fatalf("p2 status = %q, want expired", g.Status)
	}
}

// TestDestroyPolicy removes the token and all of its grants.
func TestDestroyPolicy(t *testing.T) {
	s := openTemp(t)
	token, _ := s.CreatePolicy()
	p, _ := s.CreateProposal(token, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	_, _ = s.Approve(p.ID, time.Hour)
	n, err := s.DestroyPolicy(token)
	if err != nil || n != 1 {
		t.Fatalf("DestroyPolicy: %v n=%d", err, n)
	}
	if s.PolicyExists(token) {
		t.Fatal("token still exists after destroy")
	}
}

// TestPurgeExpired deletes elapsed grants and keeps live ones.
func TestPurgeExpired(t *testing.T) {
	s := openTemp(t)
	live, _ := s.CreatePolicy()
	pl, _ := s.CreateProposal(live, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	_, _ = s.Approve(pl.ID, time.Hour)

	dead, _ := s.CreatePolicy()
	pd, _ := s.CreateProposal(dead, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	_, _ = s.Approve(pd.ID, time.Millisecond)

	s.now = func() time.Time { return time.Now().Add(time.Minute) }
	n, err := s.PurgeExpired()
	if err != nil || n != 1 {
		t.Fatalf("PurgeExpired: %v n=%d, want 1", err, n)
	}
}

// TestStartCleanupPurges runs the janitor and observes a purge callback.
func TestStartCleanupPurges(t *testing.T) {
	s := openTemp(t)
	token, _ := s.CreatePolicy()
	p, _ := s.CreateProposal(token, "me@example.com", []proposal.SignedGrantRequest{item()}, time.Hour)
	_, _ = s.Approve(p.ID, time.Millisecond)
	s.now = func() time.Time { return time.Now().Add(time.Minute) }

	stop := make(chan struct{})
	defer close(stop)
	done := make(chan int, 1)
	s.StartCleanup(stop, 5*time.Millisecond, func(n int) {
		select {
		case done <- n:
		default:
		}
	})
	select {
	case n := <-done:
		if n < 1 {
			t.Fatalf("purged %d, want >=1", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("janitor did not purge in time")
	}
}
