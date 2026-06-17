package asclient

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clems4ever/granular/internal/verify"
)

// TestVerifySignsBody checks the client signs the body with the gateway secret and
// returns the AS decision; the stub AS verifies the signature before answering.
func TestVerifySignsBody(t *testing.T) {
	secret := []byte("s3cret")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("X-Gateway-ID") != "gw" {
			t.Errorf("gateway id = %q", r.Header.Get("X-Gateway-ID"))
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write(body)
		if r.Header.Get("X-Gateway-Signature") != hex.EncodeToString(mac.Sum(nil)) {
			t.Error("signature mismatch")
		}
		_ = json.NewEncoder(w).Encode(verify.Output{Allowed: true})
	}))
	defer ts.Close()

	c := New(ts.URL, "gw", secret)
	allowed, err := c.Verify(context.Background(), verify.Input{Token: "tok"})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !allowed {
		t.Fatal("allowed = false, want true")
	}
}
