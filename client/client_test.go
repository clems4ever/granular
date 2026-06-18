package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/resourceserver"
)

const rsSecret = "s3cret"

// fakeResourceServer is an httptest server mimicking a resource server: it serves a one-action schema,
// signs grant requests with rsSecret, and executes operations subject to an allow flag.
//
// @arg t The test handle.
// @arg id The resource server id used when signing.
// @arg allow Whether the operations endpoint authorizes (200) or denies (403).
// @return *httptest.Server The running fake resource server.
//
// @testcase TestRunExecutesWhenAuthorized drives an allowing resource server.
func fakeResourceServer(t *testing.T, id string, allow bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(resourceserver.Schema{
			Resources: []resourceserver.ResourceType{{Name: "t.repo", Entity: "T::Repo"}},
			Actions:   []resourceserver.Action{{Name: "repo.read", Title: "Read"}},
		})
	})
	mux.HandleFunc("POST /api/grant-requests/sign", func(w http.ResponseWriter, r *http.Request) {
		var req resourceserver.GrantRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		pres := proposal.Presentation{Title: "t", Summary: req.Reason}
		_ = json.NewEncoder(w).Encode(proposal.Sign([]byte(rsSecret), id, pres, []string{"permit;"}))
	})
	mux.HandleFunc("POST /api/operations", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !allow {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "unauthorized"})
			return
		}
		_ = json.NewEncoder(w).Encode(Result{Status: "completed", Result: map[string]any{"ok": true}})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// fakeAS is an httptest server mimicking the AS subject and proposal endpoints. It
// records the most recent proposal it received.
//
// @arg t The test handle.
// @arg got A sink the server writes the received proposal into.
// @return *httptest.Server The running fake AS.
//
// @testcase TestSubmitSendsBundle inspects the received proposal.
func fakeAS(t *testing.T, got *proposalSubmit) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/subject", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(subjectResult{Token: "tok"})
	})
	mux.HandleFunc("GET /api/subject/{token}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(subjectResult{Grants: []Grant{{ResourceServerID: "g1", ExpiresAt: "soon"}}})
	})
	mux.HandleFunc("DELETE /api/subject/{token}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"destroyed": 2})
	})
	mux.HandleFunc("GET /api/subject/me", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(subjectResult{Grants: []Grant{{ResourceServerID: "g1", ExpiresAt: "soon"}}})
	})
	mux.HandleFunc("DELETE /api/subject/me/grants", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"revoked": 2})
	})
	mux.HandleFunc("GET /api/activity", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Activity{
			Grants:  []Grant{{SubjectToken: "tok", ResourceServerID: "g1", ExpiresAt: "soon"}},
			History: []HistoryEntry{{SubjectToken: "tok", Approver: "me@example.com", Status: "approved", Summary: "do x", Items: 1}},
		})
	})
	mux.HandleFunc("POST /api/proposals", func(w http.ResponseWriter, r *http.Request) {
		if got != nil {
			_ = json.NewDecoder(r.Body).Decode(got)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(proposalResult{ProposalID: "p1", URL: "http://as/proposal/p1"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// readCap is the capability the proposal tests request.
//
// @return resourceserver.GrantRequest A one-capability grant request.
//
// @testcase TestSubmitSendsBundle proposes this request.
func readCap() resourceserver.GrantRequest {
	return resourceserver.GrantRequest{Reason: "work", Capabilities: []resourceserver.Capability{{
		Actions:  []string{"repo.read"},
		Resource: resourceserver.ResourceSelector{Type: "t.repo", Match: map[string]string{"owner": "o", "name": "r"}},
	}}}
}

// TestSchemasFiltersResourceServers fetches all configured resource servers and a named subset.
func TestSchemasFiltersResourceServers(t *testing.T) {
	g1, g2 := fakeResourceServer(t, "g1", true), fakeResourceServer(t, "g2", true)
	c := New(Config{ResourceServers: []ResourceServer{{ID: "g1", BaseURL: g1.URL + "/"}, {ID: "g2", BaseURL: g2.URL}}})

	if ids := c.ResourceServerIDs(); len(ids) != 2 || ids[0] != "g1" || ids[1] != "g2" {
		t.Fatalf("ids = %v", ids)
	}
	all, err := c.Schemas(context.Background())
	if err != nil || len(all) != 2 {
		t.Fatalf("all schemas: %v %v", all, err)
	}
	if !all["g1"].HasAction("repo.read") {
		t.Fatal("g1 schema missing action")
	}
	sub, err := c.Schemas(context.Background(), "g2")
	if err != nil || len(sub) != 1 || sub["g2"].Resources[0].Entity != "T::Repo" {
		t.Fatalf("subset: %v %v", sub, err)
	}
	if _, err := c.Schemas(context.Background(), "ghost"); !errors.Is(err, ErrUnknownResourceServer) {
		t.Fatalf("want ErrUnknownResourceServer, got %v", err)
	}
}

// TestRunExecutesWhenAuthorized returns the result when the resource server authorizes.
func TestRunExecutesWhenAuthorized(t *testing.T) {
	g1 := fakeResourceServer(t, "g1", true)
	c := New(Config{Token: "tok", ResourceServers: []ResourceServer{{ID: "g1", BaseURL: g1.URL}}})
	res, err := c.Run(context.Background(), "g1", resourceserver.OperationRequest{Type: "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != "completed" || res.Result["ok"] != true {
		t.Fatalf("result = %+v", res)
	}
}

// TestRunNotAuthorized returns ErrNotAuthorized when the resource server denies.
func TestRunNotAuthorized(t *testing.T) {
	g1 := fakeResourceServer(t, "g1", false)
	c := New(Config{Token: "tok", ResourceServers: []ResourceServer{{ID: "g1", BaseURL: g1.URL}}})
	if _, err := c.Run(context.Background(), "g1", resourceserver.OperationRequest{Type: "x"}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("want ErrNotAuthorized, got %v", err)
	}
}

// TestRunSurfacesErrorBody checks an unexpected status surfaces the resource
// server's {"error": ...} body (e.g. a failed authorization check) in the error.
func TestRunSurfacesErrorBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/operations", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization check failed: connection refused"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := New(Config{Token: "tok", ResourceServers: []ResourceServer{{ID: "g1", BaseURL: ts.URL}}})
	_, err := c.Run(context.Background(), "g1", resourceserver.OperationRequest{Type: "x"})
	if err == nil {
		t.Fatal("expected an error on 502")
	}
	if !strings.Contains(err.Error(), "authorization check failed: connection refused") {
		t.Fatalf("error should surface the body, got %v", err)
	}
}

// TestRunUnknownResourceServer errors with ErrUnknownResourceServer (and ErrNoToken without a token).
func TestRunUnknownResourceServer(t *testing.T) {
	c := New(Config{Token: "tok", ResourceServers: []ResourceServer{{ID: "g1", BaseURL: "http://x"}}})
	if _, err := c.Run(context.Background(), "ghost", resourceserver.OperationRequest{Type: "x"}); !errors.Is(err, ErrUnknownResourceServer) {
		t.Fatalf("want ErrUnknownResourceServer, got %v", err)
	}
	noTok := New(Config{ResourceServers: []ResourceServer{{ID: "g1", BaseURL: "http://x"}}})
	if _, err := noTok.Run(context.Background(), "g1", resourceserver.OperationRequest{Type: "x"}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestSignReturnsSignedRequest signs a bundle at a resource server and checks it verifies.
func TestSignReturnsSignedRequest(t *testing.T) {
	g1 := fakeResourceServer(t, "g1", true)
	c := New(Config{ResourceServers: []ResourceServer{{ID: "g1", BaseURL: g1.URL}}})
	signed, err := c.Sign(context.Background(), "g1", readCap())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if signed.ResourceServerID != "g1" || !signed.Verify([]byte(rsSecret)) {
		t.Fatalf("signed request invalid: %+v", signed)
	}
}

// TestSignUnknownResourceServer errors when signing against an unconfigured resource server.
func TestSignUnknownResourceServer(t *testing.T) {
	c := New(Config{ResourceServers: []ResourceServer{{ID: "g1", BaseURL: "http://x"}}})
	if _, err := c.Sign(context.Background(), "ghost", readCap()); !errors.Is(err, ErrUnknownResourceServer) {
		t.Fatalf("want ErrUnknownResourceServer, got %v", err)
	}
}

// TestSubmitSendsBundle submits a signed bundle (possibly spanning resource servers) to the AS.
func TestSubmitSendsBundle(t *testing.T) {
	var got proposalSubmit
	g1 := fakeResourceServer(t, "g1", true)
	as := fakeAS(t, &got)
	c := New(Config{ASURL: as.URL, Token: "tok", ResourceServers: []ResourceServer{{ID: "g1", BaseURL: g1.URL}}})

	signed, err := c.Sign(context.Background(), "g1", readCap())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	p, err := c.Submit(context.Background(), "approver@example.com", []proposal.SignedGrantRequest{signed})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if p.ID != "p1" || p.URL == "" {
		t.Fatalf("proposal = %+v", p)
	}
	if got.ApproverEmail != "approver@example.com" || len(got.Items) != 1 {
		t.Fatalf("AS received %+v", got)
	}
	if !got.Items[0].Verify([]byte(rsSecret)) || got.Items[0].ResourceServerID != "g1" {
		t.Fatal("submitted item is not a valid g1 signature")
	}

	noTok := New(Config{ASURL: as.URL, ResourceServers: []ResourceServer{{ID: "g1", BaseURL: g1.URL}}})
	if _, err := noTok.Submit(context.Background(), "a@b.c", []proposal.SignedGrantRequest{signed}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestCreateSubjectReturnsToken mints a subject with the admin token without changing the
// configured admin token.
func TestCreateSubjectReturnsToken(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "admin"})
	tok, err := c.CreateSubject(context.Background())
	if err != nil || tok != "tok" {
		t.Fatalf("create: %q %v", tok, err)
	}
	if c.Token() != "admin" {
		t.Fatalf("admin token changed: %q", c.Token())
	}
	// Without an admin token, creation is refused.
	if _, err := New(Config{ASURL: as.URL}).CreateSubject(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestSubjectReadsGrants lists the grants attached to a named subject token.
func TestSubjectReadsGrants(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "admin"})
	grants, err := c.Subject(context.Background(), "somesubject")
	if err != nil || len(grants) != 1 || grants[0].ResourceServerID != "g1" {
		t.Fatalf("grants: %v %v", grants, err)
	}
}

// TestDestroySubject destroys a named subject and reports the number removed.
func TestDestroySubject(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "admin"})
	n, err := c.DestroySubject(context.Background(), "somesubject")
	if err != nil || n != 2 {
		t.Fatalf("destroy: %d %v", n, err)
	}
}

// TestMySubjectReturnsOwnGrants lists the caller's own grants via the subject token.
func TestMySubjectReturnsOwnGrants(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "subjecttok"})
	grants, err := c.MySubject(context.Background())
	if err != nil || len(grants) != 1 || grants[0].ResourceServerID != "g1" {
		t.Fatalf("grants: %v %v", grants, err)
	}
	// Without a subject token there is nothing to introspect.
	if _, err := New(Config{ASURL: as.URL}).MySubject(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestRevokeMyGrantsRevokesOwnGrants revokes the caller's own grants via the subject token
// and reports how many were removed.
func TestRevokeMyGrantsRevokesOwnGrants(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "subjecttok"})
	n, err := c.RevokeMyGrants(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("revoke: %d %v", n, err)
	}
}

// TestRevokeMyGrantsRequiresToken fails with ErrNoToken when no subject token is configured.
func TestRevokeMyGrantsRequiresToken(t *testing.T) {
	as := fakeAS(t, nil)
	if _, err := New(Config{ASURL: as.URL}).RevokeMyGrants(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestActivityReturnsInventory reads the cross-subject grant inventory and history.
func TestActivityReturnsInventory(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "admin"})
	act, err := c.Activity(context.Background())
	if err != nil {
		t.Fatalf("activity: %v", err)
	}
	if len(act.Grants) != 1 || act.Grants[0].SubjectToken != "tok" {
		t.Fatalf("grants: %+v", act.Grants)
	}
	if len(act.History) != 1 || act.History[0].Approver != "me@example.com" {
		t.Fatalf("history: %+v", act.History)
	}
}
