package grants

import (
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
