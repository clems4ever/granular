package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clems4ever/granular/internal/api"
)

func TestSubmitDecodesResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"pending","request_id":"abc","approval_url":"http://x/approve/abc"}`))
	}))
	defer ts.Close()

	c := New(ts.URL)
	resp, err := c.Submit(context.Background(), api.OperationRequest{Type: "test.op"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != api.StatusPending || resp.RequestID != "abc" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
