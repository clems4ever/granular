// Package grants persists delegation requests (operations awaiting human
// approval) and grants (approved, time-limited permissions) in a bbolt database
// on disk. Persistence keeps the server stateless across restarts: a decision can
// be made out-of-band and the grant survives even if the process is restarted.
// The clock and id generator are injectable for testing.
package grants

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/clems4ever/granular/internal/api"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// bucketRequests and bucketGrants name the two bbolt buckets.
var (
	bucketRequests = []byte("requests")
	bucketGrants   = []byte("grants")
)

// DelegationRequest captures an operation attempt that is waiting for a human to
// approve or reject it.
type DelegationRequest struct {
	ID            string              `json:"id"`
	OperationType string              `json:"operation_type"`
	PermissionKey string              `json:"permission_key"`
	Description   string              `json:"description"`
	Params        map[string]any      `json:"params"`
	Status        api.OperationStatus `json:"status"`
	CreatedAt     time.Time           `json:"created_at"`
}

// Grant is an approved permission for a single permission key, valid until
// ExpiresAt.
type Grant struct {
	PermissionKey string    `json:"permission_key"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// Store persists delegation requests and grants in a bbolt database.
type Store struct {
	db    *bolt.DB
	now   func() time.Time
	newID func() string
}

// Open opens (creating if needed) the bbolt database at path and ensures the
// required buckets exist.
//
// @arg path Filesystem path of the bbolt database file.
// @return *Store A ready-to-use store backed by the opened database.
// @error error when the database cannot be opened or buckets cannot be created.
//
// @testcase TestCreateAndGetRequest opens a temp-file store and round-trips a request.
func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketRequests, bucketGrants} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, now: time.Now, newID: uuid.NewString}, nil
}

// Close closes the underlying database.
//
// @return error Any error from closing the database.
//
// @testcase TestCreateAndGetRequest closes its store on cleanup.
func (s *Store) Close() error { return s.db.Close() }

// CreateRequest persists a new pending delegation request for the given operation
// metadata and returns it with a freshly generated id.
//
// @arg opType The operation type id, e.g. "github.clone".
// @arg permKey The permission key the resulting grant will be matched against.
// @arg description A human-readable summary shown on the approval page.
// @arg params The original operation parameters, retained for display/auditing.
// @return *DelegationRequest The stored request with its generated id and pending status.
// @error error when the request cannot be written to the database.
//
// @testcase TestCreateAndGetRequest checks the request is retrievable by id.
func (s *Store) CreateRequest(opType, permKey, description string, params map[string]any) (*DelegationRequest, error) {
	req := &DelegationRequest{
		ID:            s.newID(),
		OperationType: opType,
		PermissionKey: permKey,
		Description:   description,
		Params:        params,
		Status:        api.StatusPending,
		CreatedAt:     s.now(),
	}
	if err := s.put(bucketRequests, req.ID, req); err != nil {
		return nil, err
	}
	return req, nil
}

// GetRequest loads the delegation request with the given id.
//
// @arg id The delegation request id.
// @return *DelegationRequest The stored request.
// @error ErrRequestNotFound when no request has that id, or a decode/db error.
//
// @testcase TestCreateAndGetRequest retrieves an existing request.
// @testcase TestGetMissingRequest returns ErrRequestNotFound for an unknown id.
func (s *Store) GetRequest(id string) (*DelegationRequest, error) {
	var req DelegationRequest
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketRequests).Get([]byte(id))
		if v == nil {
			return ErrRequestNotFound
		}
		return json.Unmarshal(v, &req)
	})
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// Approve marks the request approved and persists a grant for its permission key
// valid for ttl from now.
//
// @arg id The delegation request id to approve.
// @arg ttl How long the resulting grant remains valid.
// @return *DelegationRequest The updated request in the approved state.
// @error ErrRequestNotFound when no request has the given id, or a db error.
//
// @testcase TestApproveCreatesLiveGrant approves a request and checks the grant.
// @testcase TestApproveMissingRequest returns ErrRequestNotFound.
func (s *Store) Approve(id string, ttl time.Duration) (*DelegationRequest, error) {
	var req DelegationRequest
	err := s.db.Update(func(tx *bolt.Tx) error {
		rb := tx.Bucket(bucketRequests)
		v := rb.Get([]byte(id))
		if v == nil {
			return ErrRequestNotFound
		}
		if err := json.Unmarshal(v, &req); err != nil {
			return err
		}
		req.Status = api.StatusApproved
		if err := putTx(rb, req.ID, &req); err != nil {
			return err
		}
		grant := Grant{PermissionKey: req.PermissionKey, ExpiresAt: s.now().Add(ttl)}
		return putTx(tx.Bucket(bucketGrants), grant.PermissionKey, grant)
	})
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// Reject marks the request rejected without creating any grant.
//
// @arg id The delegation request id to reject.
// @return *DelegationRequest The updated request in the rejected state.
// @error ErrRequestNotFound when no request has the given id, or a db error.
//
// @testcase TestRejectRequest sets the status to rejected and grants nothing.
func (s *Store) Reject(id string) (*DelegationRequest, error) {
	var req DelegationRequest
	err := s.db.Update(func(tx *bolt.Tx) error {
		rb := tx.Bucket(bucketRequests)
		v := rb.Get([]byte(id))
		if v == nil {
			return ErrRequestNotFound
		}
		if err := json.Unmarshal(v, &req); err != nil {
			return err
		}
		req.Status = api.StatusRejected
		return putTx(rb, req.ID, &req)
	})
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// HasLiveGrant reports whether a non-expired grant exists for the permission key,
// deleting the grant if it has expired.
//
// @arg permKey The permission key to check.
// @return bool True when a grant exists and has not expired.
// @error error when the database cannot be read or updated.
//
// @testcase TestApproveCreatesLiveGrant checks a fresh grant is live.
// @testcase TestExpiredGrantIsNotLive checks an elapsed grant is reported dead.
func (s *Store) HasLiveGrant(permKey string) (bool, error) {
	var live bool
	err := s.db.Update(func(tx *bolt.Tx) error {
		gb := tx.Bucket(bucketGrants)
		v := gb.Get([]byte(permKey))
		if v == nil {
			return nil
		}
		var grant Grant
		if err := json.Unmarshal(v, &grant); err != nil {
			return err
		}
		if s.now().Before(grant.ExpiresAt) {
			live = true
			return nil
		}
		return gb.Delete([]byte(permKey))
	})
	return live, err
}

// put writes value as JSON under key in the named bucket in its own transaction.
//
// @arg bucket The bucket name to write into.
// @arg key The key to write.
// @arg value The value to JSON-encode and store.
// @error error when encoding or the write transaction fails.
//
// @testcase TestCreateAndGetRequest exercises put via CreateRequest.
func (s *Store) put(bucket []byte, key string, value any) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return putTx(tx.Bucket(bucket), key, value)
	})
}

// putTx writes value as JSON under key within an existing bucket handle.
//
// @arg b The bucket to write into.
// @arg key The key to write.
// @arg value The value to JSON-encode and store.
// @error error when encoding or the bucket write fails.
//
// @testcase TestApproveCreatesLiveGrant exercises putTx via Approve.
func putTx(b *bolt.Bucket, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return b.Put([]byte(key), encoded)
}

// ErrRequestNotFound is returned when an operation references an unknown
// delegation request id.
var ErrRequestNotFound = errors.New("delegation request not found")
