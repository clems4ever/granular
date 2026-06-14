package grants

import (
	"errors"
	"path/filepath"
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
		return "id-" + string(rune('0'+counter))
	}
	return s, &clock
}

func mustCreate(t *testing.T, s *Store, permKey string) *DelegationRequest {
	t.Helper()
	req, err := s.CreateRequest("github.clone", permKey, "desc", map[string]any{"repo": "a/b"})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	return req
}

func mustHaveLiveGrant(t *testing.T, s *Store, permKey string) bool {
	t.Helper()
	live, err := s.HasLiveGrant(permKey)
	if err != nil {
		t.Fatalf("has live grant: %v", err)
	}
	return live
}

func TestCreateAndGetRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "key")
	if req.Status != api.StatusPending {
		t.Fatalf("want pending, got %s", req.Status)
	}
	got, err := s.GetRequest(req.ID)
	if err != nil || got.ID != req.ID {
		t.Fatalf("request not retrievable: %v", err)
	}
}

func TestGetMissingRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	if _, err := s.GetRequest("nope"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("want ErrRequestNotFound, got %v", err)
	}
}

func TestApproveCreatesLiveGrant(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "key")
	if _, err := s.Approve(req.ID, time.Hour); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if !mustHaveLiveGrant(t, s, "key") {
		t.Fatalf("expected live grant")
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

func TestExpiredGrantIsNotLive(t *testing.T) {
	s, clock := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "key")
	if _, err := s.Approve(req.ID, time.Minute); err != nil {
		t.Fatalf("approve: %v", err)
	}
	*clock = clock.Add(2 * time.Minute)
	if mustHaveLiveGrant(t, s, "key") {
		t.Fatalf("expected grant to be expired")
	}
}

func TestRejectRequest(t *testing.T) {
	s, _ := fixedStore(t, time.Unix(0, 0))
	req := mustCreate(t, s, "key")
	if _, err := s.Reject(req.ID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if mustHaveLiveGrant(t, s, "key") {
		t.Fatalf("reject must not create a grant")
	}
	if got, _ := s.GetRequest(req.ID); got.Status != api.StatusRejected {
		t.Fatalf("want rejected, got %s", got.Status)
	}
}

func TestGrantSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	req, err := s.CreateRequest("github.clone", "key", "desc", nil)
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
	live, err := reopened.HasLiveGrant("key")
	if err != nil {
		t.Fatalf("has live grant: %v", err)
	}
	if !live {
		t.Fatalf("grant did not survive reopen")
	}
}
