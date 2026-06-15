// Package grants persists grant requests (operations awaiting human approval)
// and approved Cedar policies in a bbolt database on disk. Authorization is decided
// by evaluating the active (non-expired) policies with the Cedar engine; this
// package only stores and expires them. The clock and id generator are injectable
// for testing.
package grants

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
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

// GrantRequest captures an operation (or custom grant request) waiting
// for a human to approve or reject it. ProposedPolicies are the Cedar policies that
// approval would store.
type GrantRequest struct {
	ID               string              `json:"id"`
	OperationType    string              `json:"operation_type"`
	Description      string              `json:"description"`
	Params           map[string]any      `json:"params"`
	ProposedPolicies []string            `json:"proposed_policies"`
	Status           api.OperationStatus `json:"status"`
	CreatedAt        time.Time           `json:"created_at"`
}

// Grant is an approved Cedar policy with an expiry. RequestID links it back
// to the grant request that produced it, so a whole request's grants can be
// revoked together.
type Grant struct {
	ID            string    `json:"id"`
	RequestID     string    `json:"request_id"`
	OperationType string    `json:"operation_type"`
	Policy        string    `json:"policy"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// Store persists grant requests and approved policies in a bbolt database.
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
// @error error Any error from closing the database.
//
// @testcase TestCreateAndGetRequest closes its store on cleanup.
func (s *Store) Close() error { return s.db.Close() }

// CreateRequest persists a new pending grant request carrying the Cedar
// policies that approval would grant.
//
// @arg opType The operation type id (or "permissions.request" for custom bundles).
// @arg description A human-readable summary shown on the approval page.
// @arg proposed The Cedar policies approval would store.
// @arg params The original operation parameters, retained for auditing.
// @return *GrantRequest The stored request with its generated id and pending status.
// @error error when the request cannot be written to the database.
//
// @testcase TestCreateAndGetRequest checks the request is retrievable by id.
func (s *Store) CreateRequest(opType, description string, proposed []string, params map[string]any) (*GrantRequest, error) {
	req := &GrantRequest{
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

// GetRequest loads the grant request with the given id.
//
// @arg id The grant request id.
// @return *GrantRequest The stored request.
// @error ErrRequestNotFound when no request has that id, or a decode/db error.
//
// @testcase TestCreateAndGetRequest retrieves an existing request.
// @testcase TestGetMissingRequest returns ErrRequestNotFound for an unknown id.
func (s *Store) GetRequest(id string) (*GrantRequest, error) {
	var req GrantRequest
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
// @arg id The grant request id to approve.
// @arg ttl How long the stored policies remain valid.
// @return *GrantRequest The updated request in the approved state.
// @error ErrRequestNotFound when no request has the given id, or a db error.
//
// @testcase TestApproveStoresActivePolicies approves and checks active policies.
// @testcase TestApproveMissingRequest returns ErrRequestNotFound.
func (s *Store) Approve(id string, ttl time.Duration) (*GrantRequest, error) {
	var req GrantRequest
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
			sp := Grant{
				ID:            s.newID(),
				RequestID:     req.ID,
				OperationType: req.OperationType,
				Policy:        policy,
				Description:   req.Description,
				CreatedAt:     s.now(),
				ExpiresAt:     s.now().Add(ttl),
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
// @arg id The grant request id to reject.
// @return *GrantRequest The updated request in the rejected state.
// @error ErrRequestNotFound when no request has the given id, or a db error.
//
// @testcase TestRejectRequest sets the status to rejected and grants nothing.
func (s *Store) Reject(id string) (*GrantRequest, error) {
	var req GrantRequest
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
			var sp Grant
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

// ListRequests returns every grant request, newest first.
//
// @return []GrantRequest All stored grant requests, sorted by creation time descending.
// @error error when the database cannot be read or a record cannot be decoded.
//
// @testcase TestListRequestsAndGrants lists requests after creating some.
func (s *Store) ListRequests() ([]GrantRequest, error) {
	var out []GrantRequest
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketRequests).ForEach(func(_, v []byte) error {
			var req GrantRequest
			if err := json.Unmarshal(v, &req); err != nil {
				return err
			}
			out = append(out, req)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// ListGrants returns every active (non-expired) stored policy, newest first,
// deleting any that have expired as a side effect.
//
// @return []Grant The active grants, sorted by creation time descending.
// @error error when the database cannot be read or updated.
//
// @testcase TestListRequestsAndGrants lists grants after an approval.
// @testcase TestRevokeGrantByID removes a grant so it no longer lists.
func (s *Store) ListGrants() ([]Grant, error) {
	var out []Grant
	err := s.db.Update(func(tx *bolt.Tx) error {
		pb := tx.Bucket(bucketPolicies)
		var expired [][]byte
		err := pb.ForEach(func(k, v []byte) error {
			var sp Grant
			if err := json.Unmarshal(v, &sp); err != nil {
				return err
			}
			if s.now().Before(sp.ExpiresAt) {
				out = append(out, sp)
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
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Revoke immediately invalidates access identified by id. The id may be a single
// grant (stored policy) id, in which case just that grant is deleted, or a
// grant request id, in which case every grant produced by that request is
// deleted and the request itself is marked revoked (even when it had no live
// grants, e.g. a still-pending request). It returns the number of grants removed
// and whether anything — a grant or a request — matched the id.
//
// @arg id A grant (stored policy) id, or a grant request id.
// @return int The number of active grants that were revoked.
// @return bool True when a grant or a request matched the id.
// @error error when the database cannot be updated.
//
// @testcase TestRevokeGrantByID revokes a single grant by its policy id.
// @testcase TestRevokeByRequestID revokes all grants of a request and marks it revoked.
// @testcase TestRevokePendingRequest revokes a pending request that has no grants.
func (s *Store) Revoke(id string) (int, bool, error) {
	revoked := 0
	found := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		pb := tx.Bucket(bucketPolicies)
		// Direct hit: the id is a stored-policy id.
		if v := pb.Get([]byte(id)); v != nil {
			if err := pb.Delete([]byte(id)); err != nil {
				return err
			}
			revoked = 1
			found = true
			return nil
		}
		// Otherwise treat id as a request id: delete all of its grants.
		var keys [][]byte
		err := pb.ForEach(func(k, v []byte) error {
			var sp Grant
			if err := json.Unmarshal(v, &sp); err != nil {
				return err
			}
			if sp.RequestID == id {
				keys = append(keys, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range keys {
			if err := pb.Delete(k); err != nil {
				return err
			}
			revoked++
		}
		// Mark the originating request revoked, if it still exists and is still
		// in an actionable (pending or approved) state.
		rb := tx.Bucket(bucketRequests)
		if rv := rb.Get([]byte(id)); rv != nil {
			found = true
			var req GrantRequest
			if err := json.Unmarshal(rv, &req); err != nil {
				return err
			}
			if req.Status == api.StatusPending || req.Status == api.StatusApproved {
				req.Status = api.StatusRevoked
				if err := putTx(rb, req.ID, &req); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return revoked, found, err
}

// PurgeExpired deletes every stored policy whose expiry has passed and returns how
// many were removed. It is what the background janitor calls on each tick.
//
// @return int The number of expired grants deleted.
// @error error when the database cannot be updated.
//
// @testcase TestPurgeExpired deletes elapsed grants and keeps live ones.
func (s *Store) PurgeExpired() (int, error) {
	purged := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		pb := tx.Bucket(bucketPolicies)
		var expired [][]byte
		err := pb.ForEach(func(k, v []byte) error {
			var sp Grant
			if err := json.Unmarshal(v, &sp); err != nil {
				return err
			}
			if !s.now().Before(sp.ExpiresAt) {
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
			purged++
		}
		return nil
	})
	return purged, err
}

// StartCleanup launches a background goroutine that calls PurgeExpired on the given
// interval until ctx is cancelled. onPurge, when non-nil, is invoked with the count
// each time a tick removes one or more grants (used for logging).
//
// @arg ctx Context whose cancellation stops the janitor.
// @arg interval How often to purge expired grants.
// @arg onPurge Optional callback invoked with the number of grants removed on a tick.
//
// @testcase TestStartCleanupPurges runs the loop and observes a purge callback.
func (s *Store) StartCleanup(ctx context.Context, interval time.Duration, onPurge func(int)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := s.PurgeExpired()
				if err == nil && n > 0 && onPurge != nil {
					onPurge(n)
				}
			}
		}
	}()
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
// grant request id.
var ErrRequestNotFound = errors.New("grant request not found")
