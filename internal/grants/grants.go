// Package grants persists delegation requests (operations awaiting human approval)
// and approved Cedar policies in a bbolt database on disk. Authorization is decided
// by evaluating the active (non-expired) policies with the Cedar engine; this
// package only stores and expires them. The clock and id generator are injectable
// for testing.
package grants

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/clems4ever/granular/internal/api"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// bucketRequests and bucketPolicies name the two bbolt buckets.
var (
	bucketRequests = []byte("requests")
	bucketPolicies = []byte("policies")
)

// DelegationRequest captures an operation (or custom permissions request) waiting
// for a human to approve or reject it. ProposedPolicies are the Cedar policies that
// approval would store.
type DelegationRequest struct {
	ID               string              `json:"id"`
	OperationType    string              `json:"operation_type"`
	Description      string              `json:"description"`
	Params           map[string]any      `json:"params"`
	ProposedPolicies []string            `json:"proposed_policies"`
	Status           api.OperationStatus `json:"status"`
	CreatedAt        time.Time           `json:"created_at"`
}

// StoredPolicy is an approved Cedar policy with an expiry.
type StoredPolicy struct {
	ID          string    `json:"id"`
	Policy      string    `json:"policy"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Store persists delegation requests and approved policies in a bbolt database.
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
		for _, name := range [][]byte{bucketRequests, bucketPolicies} {
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

// CreateRequest persists a new pending delegation request carrying the Cedar
// policies that approval would grant.
//
// @arg opType The operation type id (or "permissions.request" for custom bundles).
// @arg description A human-readable summary shown on the approval page.
// @arg proposed The Cedar policies approval would store.
// @arg params The original operation parameters, retained for auditing.
// @return *DelegationRequest The stored request with its generated id and pending status.
// @error error when the request cannot be written to the database.
//
// @testcase TestCreateAndGetRequest checks the request is retrievable by id.
func (s *Store) CreateRequest(opType, description string, proposed []string, params map[string]any) (*DelegationRequest, error) {
	req := &DelegationRequest{
		ID:               s.newID(),
		OperationType:    opType,
		Description:      description,
		Params:           params,
		ProposedPolicies: proposed,
		Status:           api.StatusPending,
		CreatedAt:        s.now(),
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

// Approve marks the request approved and stores its proposed policies, each valid
// for ttl from now.
//
// @arg id The delegation request id to approve.
// @arg ttl How long the stored policies remain valid.
// @return *DelegationRequest The updated request in the approved state.
// @error ErrRequestNotFound when no request has the given id, or a db error.
//
// @testcase TestApproveStoresActivePolicies approves and checks active policies.
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
		pb := tx.Bucket(bucketPolicies)
		for _, policy := range req.ProposedPolicies {
			sp := StoredPolicy{
				ID:          s.newID(),
				Policy:      policy,
				Description: req.Description,
				CreatedAt:   s.now(),
				ExpiresAt:   s.now().Add(ttl),
			}
			if err := putTx(pb, sp.ID, sp); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &req, nil
}

// Reject marks the request rejected without storing any policy.
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

// ActivePolicies returns the Cedar text of every non-expired policy, deleting any
// that have expired.
//
// @return []string The active policy texts.
// @error error when the database cannot be read or updated.
//
// @testcase TestApproveStoresActivePolicies sees a fresh policy as active.
// @testcase TestExpiredPolicyIsDropped checks an elapsed policy is removed.
func (s *Store) ActivePolicies() ([]string, error) {
	var active []string
	err := s.db.Update(func(tx *bolt.Tx) error {
		pb := tx.Bucket(bucketPolicies)
		var expired [][]byte
		err := pb.ForEach(func(k, v []byte) error {
			var sp StoredPolicy
			if err := json.Unmarshal(v, &sp); err != nil {
				return err
			}
			if s.now().Before(sp.ExpiresAt) {
				active = append(active, sp.Policy)
			} else {
				expired = append(expired, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range expired {
			if err := pb.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
	return active, err
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
// @testcase TestApproveStoresActivePolicies exercises putTx via Approve.
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
