package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clems4ever/granular/gateway"
	"github.com/clems4ever/granular/internal/proposal"
)

const gwSecret = "s3cret"

// fakeGateway is an httptest server mimicking a gateway: it serves a one-action schema,
// signs grant requests with gwSecret, and executes operations subject to an allow flag.
//
// @arg t The test handle.
// @arg id The gateway id used when signing.
// @arg allow Whether the operations endpoint authorizes (200) or denies (403).
// @return *httptest.Server The running fake gateway.
//
// @testcase TestRunExecutesWhenAuthorized drives an allowing gateway.
func fakeGateway(t *testing.T, id string, allow bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/schema", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(gateway.Schema{
			Resources: []gateway.ResourceType{{Name: "t.repo", Entity: "T::Repo"}},
			Actions:   []gateway.Action{{Name: "repo.read", Title: "Read"}},
		})
	})
	mux.HandleFunc("POST /api/grant-requests/sign", func(w http.ResponseWriter, r *http.Request) {
		var req gateway.GrantRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		pres := proposal.Presentation{Title: "t", Summary: req.Reason}
		_ = json.NewEncoder(w).Encode(proposal.Sign([]byte(gwSecret), id, pres, []string{"permit;"}))
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

// fakeAS is an httptest server mimicking the AS policy and proposal endpoints. It
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
	mux.HandleFunc("PUT /api/policy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(policyResult{Token: "tok"})
	})
	mux.HandleFunc("GET /api/policy/{token}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(policyResult{Grants: []Grant{{GatewayID: "g1", ExpiresAt: "soon"}}})
	})
	mux.HandleFunc("DELETE /api/policy/{token}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"destroyed": 2})
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
// @return gateway.GrantRequest A one-capability grant request.
//
// @testcase TestSubmitSendsBundle proposes this request.
func readCap() gateway.GrantRequest {
	return gateway.GrantRequest{Reason: "work", Capabilities: []gateway.Capability{{
		Actions:  []string{"repo.read"},
		Resource: gateway.ResourceSelector{Type: "t.repo", Match: map[string]string{"owner": "o", "name": "r"}},
	}}}
}

// TestSchemasFiltersGateways fetches all configured gateways and a named subset.
func TestSchemasFiltersGateways(t *testing.T) {
	g1, g2 := fakeGateway(t, "g1", true), fakeGateway(t, "g2", true)
	c := New(Config{Gateways: []Gateway{{ID: "g1", BaseURL: g1.URL + "/"}, {ID: "g2", BaseURL: g2.URL}}})

	if ids := c.GatewayIDs(); len(ids) != 2 || ids[0] != "g1" || ids[1] != "g2" {
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
	if _, err := c.Schemas(context.Background(), "ghost"); !errors.Is(err, ErrUnknownGateway) {
		t.Fatalf("want ErrUnknownGateway, got %v", err)
	}
}

// TestRunExecutesWhenAuthorized returns the result when the gateway authorizes.
func TestRunExecutesWhenAuthorized(t *testing.T) {
	g1 := fakeGateway(t, "g1", true)
	c := New(Config{Token: "tok", Gateways: []Gateway{{ID: "g1", BaseURL: g1.URL}}})
	res, err := c.Run(context.Background(), "g1", gateway.OperationRequest{Type: "x"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != "completed" || res.Result["ok"] != true {
		t.Fatalf("result = %+v", res)
	}
}

// TestRunNotAuthorized returns ErrNotAuthorized when the gateway denies.
func TestRunNotAuthorized(t *testing.T) {
	g1 := fakeGateway(t, "g1", false)
	c := New(Config{Token: "tok", Gateways: []Gateway{{ID: "g1", BaseURL: g1.URL}}})
	if _, err := c.Run(context.Background(), "g1", gateway.OperationRequest{Type: "x"}); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("want ErrNotAuthorized, got %v", err)
	}
}

// TestRunUnknownGateway errors with ErrUnknownGateway (and ErrNoToken without a token).
func TestRunUnknownGateway(t *testing.T) {
	c := New(Config{Token: "tok", Gateways: []Gateway{{ID: "g1", BaseURL: "http://x"}}})
	if _, err := c.Run(context.Background(), "ghost", gateway.OperationRequest{Type: "x"}); !errors.Is(err, ErrUnknownGateway) {
		t.Fatalf("want ErrUnknownGateway, got %v", err)
	}
	noTok := New(Config{Gateways: []Gateway{{ID: "g1", BaseURL: "http://x"}}})
	if _, err := noTok.Run(context.Background(), "g1", gateway.OperationRequest{Type: "x"}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestSignReturnsSignedRequest signs a bundle at a gateway and checks it verifies.
func TestSignReturnsSignedRequest(t *testing.T) {
	g1 := fakeGateway(t, "g1", true)
	c := New(Config{Gateways: []Gateway{{ID: "g1", BaseURL: g1.URL}}})
	signed, err := c.Sign(context.Background(), "g1", readCap())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if signed.GatewayID != "g1" || !signed.Verify([]byte(gwSecret)) {
		t.Fatalf("signed request invalid: %+v", signed)
	}
}

// TestSignUnknownGateway errors when signing against an unconfigured gateway.
func TestSignUnknownGateway(t *testing.T) {
	c := New(Config{Gateways: []Gateway{{ID: "g1", BaseURL: "http://x"}}})
	if _, err := c.Sign(context.Background(), "ghost", readCap()); !errors.Is(err, ErrUnknownGateway) {
		t.Fatalf("want ErrUnknownGateway, got %v", err)
	}
}

// TestSubmitSendsBundle submits a signed bundle (possibly spanning gateways) to the AS.
func TestSubmitSendsBundle(t *testing.T) {
	var got proposalSubmit
	g1 := fakeGateway(t, "g1", true)
	as := fakeAS(t, &got)
	c := New(Config{ASURL: as.URL, Token: "tok", Gateways: []Gateway{{ID: "g1", BaseURL: g1.URL}}})

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
	if !got.Items[0].Verify([]byte(gwSecret)) || got.Items[0].GatewayID != "g1" {
		t.Fatal("submitted item is not a valid g1 signature")
	}

	noTok := New(Config{ASURL: as.URL, Gateways: []Gateway{{ID: "g1", BaseURL: g1.URL}}})
	if _, err := noTok.Submit(context.Background(), "a@b.c", []proposal.SignedGrantRequest{signed}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestCreatePolicyReturnsToken mints a policy with the admin token without changing the
// configured admin token.
func TestCreatePolicyReturnsToken(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "admin"})
	tok, err := c.CreatePolicy(context.Background())
	if err != nil || tok != "tok" {
		t.Fatalf("create: %q %v", tok, err)
	}
	if c.Token() != "admin" {
		t.Fatalf("admin token changed: %q", c.Token())
	}
	// Without an admin token, creation is refused.
	if _, err := New(Config{ASURL: as.URL}).CreatePolicy(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

// TestPolicyReadsGrants lists the grants attached to a named policy token.
func TestPolicyReadsGrants(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "admin"})
	grants, err := c.Policy(context.Background(), "somepolicy")
	if err != nil || len(grants) != 1 || grants[0].GatewayID != "g1" {
		t.Fatalf("grants: %v %v", grants, err)
	}
}

// TestDestroyPolicy destroys a named policy and reports the number removed.
func TestDestroyPolicy(t *testing.T) {
	as := fakeAS(t, nil)
	c := New(Config{ASURL: as.URL, Token: "admin"})
	n, err := c.DestroyPolicy(context.Background(), "somepolicy")
	if err != nil || n != 2 {
		t.Fatalf("destroy: %d %v", n, err)
	}
}
