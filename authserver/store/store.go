// Package store persists the authorization server's state in a bbolt database:
// subjects (each identified by a token), pending proposals awaiting human approval,
// and the approved grants attached to a subject token. It is domain-agnostic — grants
// carry opaque, resource server-signed policy blobs the store never interprets. The clock and
// id generator are injectable for testing.
//
// A token *represents a subject*: PUT mints one, grants attach to it on approval, GET
// reads it, DELETE destroys it.
package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/clems4ever/granular/internal/proposal"
	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

// Bucket names: subject tokens, pending proposals, and approved grants.
var (
	bucketSubjects  = []byte("subjects")
	bucketProposals = []byte("proposals")
	bucketGrants    = []byte("grants")
)

// Status is the lifecycle state of a proposal.
type Status string

const (
	// StatusPending means the proposal awaits human approval.
	StatusPending Status = "pending"
	// StatusApproved means a human approved the proposal and its grants are live.
	StatusApproved Status = "approved"
	// StatusRejected means a human denied the proposal.
	StatusRejected Status = "rejected"
	// StatusExpired means the proposal elapsed its pending window before a human
	// decided it, so it was automatically revoked and can no longer be approved.
	StatusExpired Status = "expired"
)

// subjectRecord is the metadata for a subject token. The grants attached to the token
// form the subject's content.
type subjectRecord struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

// Proposal is a bundle of resource server-signed grant requests a holder submitted for
// approval against its subject token. ApproverEmail names the human who must sign in
// to decide it. A proposal is only approvable while pending and before ExpiresAt; past
// that it is automatically revoked (StatusExpired).
type Proposal struct {
	ID            string                        `json:"id"`
	Token         string                        `json:"token"`
	ApproverEmail string                        `json:"approver_email"`
	Items         []proposal.SignedGrantRequest `json:"items"`
	Status        Status                        `json:"status"`
	CreatedAt     time.Time                     `json:"created_at"`
	ExpiresAt     time.Time                     `json:"expires_at"`
}

// Expired reports whether the proposal is still pending but its approval window has
// elapsed as of now, so it should be treated as automatically revoked.
//
// @arg now The reference time.
// @return bool True when the proposal is pending and past its expiry.
//
// @testcase TestProposalExpires reports a lapsed pending proposal as expired.
func (p *Proposal) Expired(now time.Time) bool {
	return p.Status == StatusPending && !now.Before(p.ExpiresAt)
}

// Grant is one approved, time-limited grant attached to a subject token. Item carries
// the opaque, resource server-signed policies a resource server evaluates at enforcement.
type Grant struct {
	ID         string                      `json:"id"`
	Token      string                      `json:"token"`
	ProposalID string                      `json:"proposal_id"`
	Item       proposal.SignedGrantRequest `json:"item"`
	CreatedAt  time.Time                   `json:"created_at"`
	ExpiresAt  time.Time                   `json:"expires_at"`
}

// Store persists subjects, proposals and grants in a bbolt database.
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
// @testcase TestSubjectLifecycle opens a temp-file store.
func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{bucketSubjects, bucketProposals, bucketGrants} {
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
// @testcase TestSubjectLifecycle closes its store on cleanup.
func (s *Store) Close() error { return s.db.Close() }

// CreateSubject mints a new subject with a fresh random token (PUT /api/subject) and
// returns the token. The subject starts empty; grants attach to it on approval.
//
// @return string The new subject token.
// @error error when a token cannot be generated or persisted.
//
// @testcase TestSubjectLifecycle creates a subject and uses its token.
func (s *Store) CreateSubject() (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.put(bucketSubjects, token, subjectRecord{Token: token, CreatedAt: s.now()}); err != nil {
		return "", err
	}
	return token, nil
}

// SubjectExists reports whether token identifies an existing subject.
//
// @arg token The bearer token presented by a caller.
// @return bool True when the token identifies a known subject.
//
// @testcase TestSubjectLifecycle checks a known and an unknown token.
func (s *Store) SubjectExists(token string) bool {
	exists := false
	_ = s.db.View(func(tx *bolt.Tx) error {
		exists = token != "" && tx.Bucket(bucketSubjects).Get([]byte(token)) != nil
		return nil
	})
	return exists
}

// CreateProposal records a pending proposal against the subject token, carrying the
// signed items and the approver's email. The proposal expires ttl after creation; once
// expired it is automatically revoked and can no longer be approved.
//
// @arg token The subject the approved grants will attach to.
// @arg approverEmail The human who must sign in to decide the proposal.
// @arg items The resource server-signed grant requests bundled by the client.
// @arg ttl How long the proposal may stay pending before it is automatically revoked.
// @return *Proposal The stored proposal with its generated id, pending status and expiry.
// @error error when the proposal cannot be written.
//
// @testcase TestProposalApprovalAttachesGrants creates and approves a proposal.
// @testcase TestProposalExpires creates a proposal that lapses.
func (s *Store) CreateProposal(token, approverEmail string, items []proposal.SignedGrantRequest, ttl time.Duration) (*Proposal, error) {
	now := s.now()
	p := &Proposal{
		ID:            s.newID(),
		Token:         token,
		ApproverEmail: approverEmail,
		Items:         items,
		Status:        StatusPending,
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl),
	}
	if err := s.put(bucketProposals, p.ID, p); err != nil {
		return nil, err
	}
	return p, nil
}

// GetProposal loads the proposal with the given id.
//
// @arg id The proposal id.
// @return *Proposal The stored proposal.
// @error ErrNotFound when no proposal has that id, or a decode/db error.
//
// @testcase TestProposalApprovalAttachesGrants retrieves a proposal by id.
// @testcase TestGetMissingProposal returns ErrNotFound for an unknown id.
func (s *Store) GetProposal(id string) (*Proposal, error) {
	var p Proposal
	err := s.db.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(bucketProposals).Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		return json.Unmarshal(v, &p)
	})
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Approve marks the proposal approved and attaches one grant per item to its subject
// token, each valid for ttl from now.
//
// @arg id The proposal id to approve.
// @arg ttl How long the attached grants remain valid.
// @return *Proposal The updated proposal in the approved state.
// @error ErrNotFound when no proposal has the id; ErrAlreadyDecided when not pending.
//
// @testcase TestProposalApprovalAttachesGrants approves and reads back the subject's grants.
// @testcase TestApproveTwiceFails rejects approving an already-decided proposal.
// @testcase TestProposalExpires refuses to approve a lapsed proposal.
func (s *Store) Approve(id string, ttl time.Duration) (*Proposal, error) {
	var p Proposal
	expired := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		pb := tx.Bucket(bucketProposals)
		v := pb.Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(v, &p); err != nil {
			return err
		}
		if p.Status != StatusPending {
			return ErrAlreadyDecided
		}
		// Auto-revoke a lapsed request: commit the expired status (returning an error
		// here would roll the change back) and signal ErrExpired to the caller below.
		if p.Expired(s.now()) {
			expired = true
			p.Status = StatusExpired
			return putTx(pb, p.ID, &p)
		}
		p.Status = StatusApproved
		if err := putTx(pb, p.ID, &p); err != nil {
			return err
		}
		gb := tx.Bucket(bucketGrants)
		for _, item := range p.Items {
			g := Grant{
				ID:         s.newID(),
				Token:      p.Token,
				ProposalID: p.ID,
				Item:       item,
				CreatedAt:  s.now(),
				ExpiresAt:  s.now().Add(ttl),
			}
			if err := putTx(gb, g.ID, g); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if expired {
		return &p, ErrExpired
	}
	return &p, nil
}

// Reject marks the proposal rejected without attaching any grant.
//
// @arg id The proposal id to reject.
// @return *Proposal The updated proposal in the rejected state.
// @error ErrNotFound when no proposal has the id; ErrAlreadyDecided when not pending.
//
// @testcase TestRejectProposal sets the status to rejected and attaches nothing.
func (s *Store) Reject(id string) (*Proposal, error) {
	var p Proposal
	expired := false
	err := s.db.Update(func(tx *bolt.Tx) error {
		pb := tx.Bucket(bucketProposals)
		v := pb.Get([]byte(id))
		if v == nil {
			return ErrNotFound
		}
		if err := json.Unmarshal(v, &p); err != nil {
			return err
		}
		if p.Status != StatusPending {
			return ErrAlreadyDecided
		}
		if p.Expired(s.now()) {
			expired = true
			p.Status = StatusExpired
			return putTx(pb, p.ID, &p)
		}
		p.Status = StatusRejected
		return putTx(pb, p.ID, &p)
	})
	if err != nil {
		return nil, err
	}
	if expired {
		return &p, ErrExpired
	}
	return &p, nil
}

// SubjectForToken returns the active (non-expired) grants attached to a subject token,
// deleting any that have expired as a side effect.
//
// @arg token The subject whose grants are requested.
// @return []Grant The active grants attached to the token.
// @error error when the database cannot be read or updated.
//
// @testcase TestProposalApprovalAttachesGrants reads the subject identified by a token.
// @testcase TestExpiredGrantDropped omits an elapsed grant.
func (s *Store) SubjectForToken(token string) ([]Grant, error) {
	var out []Grant
	err := s.db.Update(func(tx *bolt.Tx) error {
		gb := tx.Bucket(bucketGrants)
		var expired [][]byte
		err := gb.ForEach(func(k, v []byte) error {
			var g Grant
			if err := json.Unmarshal(v, &g); err != nil {
				return err
			}
			if g.Token != token {
				return nil
			}
			if s.now().Before(g.ExpiresAt) {
				out = append(out, g)
			} else {
				expired = append(expired, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range expired {
			if err := gb.Delete(k); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AllGrants returns every active (non-expired) grant across all subjects, for the
// authorization server's observability UI. It does not mutate the store.
//
// @return []Grant The active grants, newest first.
// @error error when the database cannot be read.
//
// @testcase TestAllGrantsAndProposals lists active grants and omits expired ones.
func (s *Store) AllGrants() ([]Grant, error) {
	var out []Grant
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketGrants).ForEach(func(_, v []byte) error {
			var g Grant
			if err := json.Unmarshal(v, &g); err != nil {
				return err
			}
			if s.now().Before(g.ExpiresAt) {
				out = append(out, g)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sortByCreatedDesc(out, func(i int) time.Time { return out[i].CreatedAt })
	return out, nil
}

// AllProposals returns every recorded proposal (the request/decision history) for the
// observability UI. It does not mutate the store.
//
// @return []Proposal The proposals, newest first.
// @error error when the database cannot be read.
//
// @testcase TestAllGrantsAndProposals lists the proposal history with statuses.
func (s *Store) AllProposals() ([]Proposal, error) {
	var out []Proposal
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketProposals).ForEach(func(_, v []byte) error {
			var p Proposal
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			out = append(out, p)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	sortByCreatedDesc(out, func(i int) time.Time { return out[i].CreatedAt })
	return out, nil
}

// sortByCreatedDesc sorts a slice in place by a creation timestamp, newest first.
//
// @arg s The slice to sort.
// @arg created Returns the creation time of element i.
//
// @testcase TestAllGrantsAndProposals checks results come back newest first.
func sortByCreatedDesc[T any](s []T, created func(i int) time.Time) {
	sort.SliceStable(s, func(i, j int) bool { return created(i).After(created(j)) })
}

// DestroySubject deletes a subject token and every grant attached to it, returning how
// many grants were removed.
//
// @arg token The subject to destroy.
// @return int The number of grants deleted.
// @error error when the database cannot be updated.
//
// @testcase TestDestroySubject removes the token and all its grants.
func (s *Store) DestroySubject(token string) (int, error) {
	deleted := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bucketSubjects).Delete([]byte(token)); err != nil {
			return err
		}
		n, err := deleteGrantsForToken(tx.Bucket(bucketGrants), token)
		deleted = n
		return err
	})
	return deleted, err
}

// RevokeGrantsForToken deletes every grant attached to a subject token while leaving the
// subject itself intact, returning how many grants were removed. Unlike DestroySubject it
// keeps the token usable, so a subject can revoke everything it currently holds in one
// step and still introspect or submit fresh proposals afterward.
//
// @arg token The subject whose grants are revoked.
// @return int The number of grants deleted.
// @error error when the database cannot be updated.
//
// @testcase TestRevokeGrantsForToken removes all of a subject's grants but keeps the token.
// @testcase TestRevokeGrantsForTokenScopedToToken leaves other subjects' grants untouched.
func (s *Store) RevokeGrantsForToken(token string) (int, error) {
	deleted := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		n, err := deleteGrantsForToken(tx.Bucket(bucketGrants), token)
		deleted = n
		return err
	})
	return deleted, err
}

// deleteGrantsForToken deletes every grant in the bucket attached to the subject token,
// returning how many were removed. It runs inside the caller's update transaction so the
// subject's grants are revoked atomically with whatever else the caller does.
//
// @arg gb The grants bucket handle from the enclosing transaction.
// @arg token The subject whose grants are deleted.
// @return int The number of grants deleted.
// @error error when the bucket cannot be scanned or a delete fails.
//
// @testcase TestDestroySubject removes a subject's grants via this helper.
// @testcase TestRevokeGrantsForToken revokes a subject's grants via this helper.
func deleteGrantsForToken(gb *bolt.Bucket, token string) (int, error) {
	var keys [][]byte
	err := gb.ForEach(func(k, v []byte) error {
		var g Grant
		if err := json.Unmarshal(v, &g); err != nil {
			return err
		}
		if g.Token == token {
			keys = append(keys, append([]byte(nil), k...))
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, k := range keys {
		if err := gb.Delete(k); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

// PurgeExpired deletes every grant whose expiry has passed and automatically revokes
// (marks StatusExpired) every pending proposal whose approval window has elapsed,
// returning how many items it affected. It is what the background janitor calls on each
// tick, so even un-viewed requests are revoked durably.
//
// @return int The number of grants deleted plus proposals expired.
// @error error when the database cannot be updated.
//
// @testcase TestPurgeExpired deletes elapsed grants and keeps live ones.
// @testcase TestProposalExpires has the janitor revoke a lapsed pending proposal.
func (s *Store) PurgeExpired() (int, error) {
	affected := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		now := s.now()
		gb := tx.Bucket(bucketGrants)
		var expired [][]byte
		err := gb.ForEach(func(k, v []byte) error {
			var g Grant
			if err := json.Unmarshal(v, &g); err != nil {
				return err
			}
			if !now.Before(g.ExpiresAt) {
				expired = append(expired, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range expired {
			if err := gb.Delete(k); err != nil {
				return err
			}
			affected++
		}

		pb := tx.Bucket(bucketProposals)
		var lapsed []Proposal
		err = pb.ForEach(func(_, v []byte) error {
			var p Proposal
			if err := json.Unmarshal(v, &p); err != nil {
				return err
			}
			if p.Expired(now) {
				p.Status = StatusExpired
				lapsed = append(lapsed, p)
			}
			return nil
		})
		if err != nil {
			return err
		}
		for i := range lapsed {
			if err := putTx(pb, lapsed[i].ID, &lapsed[i]); err != nil {
				return err
			}
			affected++
		}
		return nil
	})
	return affected, err
}

// StartCleanup launches a background goroutine that calls PurgeExpired on the given
// interval until stop is closed. onPurge, when non-nil, is invoked with the count
// each time a tick removes one or more grants.
//
// @arg stop A channel whose close stops the janitor.
// @arg interval How often to purge expired grants.
// @arg onPurge Optional callback invoked with the number of grants removed on a tick.
//
// @testcase TestStartCleanupPurges runs the loop and observes a purge callback.
func (s *Store) StartCleanup(stop <-chan struct{}, interval time.Duration, onPurge func(int)) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
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
// @testcase TestSubjectLifecycle exercises put via CreateSubject.
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
// @testcase TestProposalApprovalAttachesGrants exercises putTx via Approve.
func putTx(b *bolt.Bucket, key string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return b.Put([]byte(key), encoded)
}

// randomToken returns a URL-safe random 32-byte token representing a subject.
//
// @return string A base64url-encoded random token.
// @error error when the system RNG fails.
//
// @testcase TestSubjectLifecycle relies on a generated token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ErrNotFound is returned when a proposal id does not exist. ErrAlreadyDecided is
// returned when approving or rejecting a proposal that is no longer pending.
var (
	ErrNotFound       = errors.New("proposal not found")
	ErrAlreadyDecided = errors.New("proposal already decided")
	// ErrExpired is returned when deciding a proposal whose approval window has elapsed.
	ErrExpired = errors.New("proposal expired")
)
