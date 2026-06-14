package grants

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/clems4ever/granular/internal/api"
)

// fixedStore returns a temp-file store with a controllable clock and
// deterministic ids for testing.
func fixedStore(t *testing.T, now time.Time) (*Store, *time.Time) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	clock := now
	s.now = func() time.Time { return clock }
	counter := 0
	s.newID = func() string {
		counter++
		return "id-" + strconv.Itoa(counter)
	}
	return s, &clock
}

func mustCreate(t *testing.T, s *Store, proposed ...string) *DelegationRequest {
	t.Helper()
	req, err := s.CreateRequest("github.clone", "desc", proposed, map[string]any{"repo": "a/b"})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	return req
}

func mustActive(t *testing.T, s *Store) []string {
	t.Helper()
	p, err := s.ActivePolicies()
	if err != nil {
		t.Fatalf("active policies: %v", err)
	}
	return p
}

func TestCreateAndGetRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	if req.Status != api.StatusPending {
		t.Fatalf("want pending, got %s", req.Status)
	}
	got, err := s.GetRequest(req.ID)
	if err != nil || got.ID != req.ID || len(got.ProposedPolicies) != 1 {
		t.Fatalf("request not retrievable: %v %+v", err, got)
	}
}

func TestGetMissingRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	if _, err := s.GetRequest("nope"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("want ErrRequestNotFound, got %v", err)
	}
}

func TestApproveStoresActivePolicies(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	if _, err := s.Approve(req.ID, time.Hour); err != nil {
		t.Fatalf("approve: %v", err)
	}
	active := mustActive(t, s)
	if len(active) != 1 || active[0] != "permit ( principal, action, resource );" {
		t.Fatalf("unexpected active policies: %v", active)
	}
	if got, _ := s.GetRequest(req.ID); got.Status != api.StatusApproved {
		t.Fatalf("want approved, got %s", got.Status)
	}
}

func TestApproveMissingRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	if _, err := s.Approve("nope", time.Hour); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("want ErrRequestNotFound, got %v", err)
	}
}

func TestExpiredPolicyIsDropped(t *testing.T) {
	s, clock := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	if _, err := s.Approve(req.ID, time.Minute); err != nil {
		t.Fatalf("approve: %v", err)
	}
	*clock = clock.Add(2 * time.Minute)
	if active := mustActive(t, s); len(active) != 0 {
		t.Fatalf("expected no active policies, got %v", active)
	}
}

func TestRejectRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	if _, err := s.Reject(req.ID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if active := mustActive(t, s); len(active) != 0 {
		t.Fatalf("reject must not store a policy, got %v", active)
	}
	if got, _ := s.GetRequest(req.ID); got.Status != api.StatusRejected {
		t.Fatalf("want rejected, got %s", got.Status)
	}
}

func TestPolicySurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	req, err := s.CreateRequest("github.clone", "desc", []string{"permit ( principal, action, resource );"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Approve(req.ID, time.Hour); err != nil {
		t.Fatalf("approve: %v", err)
	}
	_ = s.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if active, _ := reopened.ActivePolicies(); len(active) != 1 {
		t.Fatalf("policy did not survive reopen: %v", active)
	}
}

func TestListRequestsAndGrants(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(100, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	if _, err := s.Approve(req.ID, time.Hour); err != nil {
		t.Fatalf("approve: %v", err)
	}

	reqs, err := s.ListRequests()
	if err != nil || len(reqs) != 1 || reqs[0].ID != req.ID {
		t.Fatalf("ListRequests = %+v, %v", reqs, err)
	}
	grants, err := s.ListGrants()
	if err != nil || len(grants) != 1 {
		t.Fatalf("ListGrants = %+v, %v", grants, err)
	}
	if grants[0].RequestID != req.ID || grants[0].OperationType != "github.clone" {
		t.Fatalf("grant missing request linkage: %+v", grants[0])
	}
}

func TestRevokeGrantByID(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(100, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	if _, err := s.Approve(req.ID, time.Hour); err != nil {
		t.Fatalf("approve: %v", err)
	}
	grants, _ := s.ListGrants()
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant")
	}
	n, _, err := s.Revoke(grants[0].ID)
	if err != nil || n != 1 {
		t.Fatalf("Revoke by grant id = %d, %v", n, err)
	}
	if after, _ := s.ListGrants(); len(after) != 0 {
		t.Fatalf("grant should be gone, got %+v", after)
	}
	// Unknown id revokes nothing.
	if n, found, _ := s.Revoke("nope"); n != 0 || found {
		t.Fatalf("unknown id should revoke 0, got %d", n)
	}
}

func TestRevokeByRequestID(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(100, 0))
	req := mustCreate(t, s,
		"permit ( principal, action == A, resource );",
		"permit ( principal, action == B, resource );")
	if _, err := s.Approve(req.ID, time.Hour); err != nil {
		t.Fatalf("approve: %v", err)
	}
	n, found, err := s.Revoke(req.ID)
	if err != nil || n != 2 || !found {
		t.Fatalf("Revoke by request id = %d, %v (want 2)", n, err)
	}
	if after, _ := s.ListGrants(); len(after) != 0 {
		t.Fatalf("all grants should be gone, got %+v", after)
	}
	got, _ := s.GetRequest(req.ID)
	if got.Status != api.StatusRevoked {
		t.Fatalf("request status = %s, want revoked", got.Status)
	}
}

func TestPurgeExpired(t *testing.T) {
	s, clock := fixedStore(t, time.Unix(100, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	if _, err := s.Approve(req.ID, time.Minute); err != nil {
		t.Fatalf("approve: %v", err)
	}
	// Before expiry: purge removes nothing.
	if n, _ := s.PurgeExpired(); n != 0 {
		t.Fatalf("nothing should be purged yet, got %d", n)
	}
	// Advance past expiry.
	*clock = clock.Add(2 * time.Minute)
	if n, _ := s.PurgeExpired(); n != 1 {
		t.Fatalf("expired grant should be purged, got %d", n)
	}
	if after, _ := s.ListGrants(); len(after) != 0 {
		t.Fatalf("no grants should remain, got %+v", after)
	}
}

func TestStartCleanupPurges(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "cleanup.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	req, err := s.CreateRequest("github.clone", "d", []string{"permit ( principal, action, resource );"}, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Already-expired grant.
	if _, err := s.Approve(req.ID, time.Millisecond); err != nil {
		t.Fatalf("approve: %v", err)
	}
	time.Sleep(5 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	purged := make(chan int, 1)
	s.StartCleanup(ctx, 5*time.Millisecond, func(n int) { purged <- n })

	select {
	case n := <-purged:
		if n < 1 {
			t.Fatalf("expected at least one purged, got %d", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup did not purge in time")
	}
}

func TestRevokePendingRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(100, 0))
	req := mustCreate(t, s, "permit ( principal, action, resource );")
	// Never approved: no grants exist, but the request should still be revocable.
	n, found, err := s.Revoke(req.ID)
	if err != nil || n != 0 || !found {
		t.Fatalf("Revoke pending = n %d, found %v, err %v (want 0,true,nil)", n, found, err)
	}
	got, _ := s.GetRequest(req.ID)
	if got.Status != api.StatusRevoked {
		t.Fatalf("pending request should be revoked, got %s", got.Status)
	}
}
