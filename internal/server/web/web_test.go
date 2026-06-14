package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderProducesHTML(t *testing.T) {
	cases := map[string]any{
		"index":   nil,
		"catalog": map[string]any{"ExampleJSON": "{}"},
		"approve": map[string]any{"ID": "abc", "Description": "do a thing", "Decided": false},
		"result":  map[string]any{"Status": "approved", "Message": "done"},
	}
	for name, data := range cases {
		rec := httptest.NewRecorder()
		if err := Render(rec, name, data); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "<!doctype html>") || !strings.Contains(body, "granular") {
			t.Errorf("page %s missing layout chrome", name)
		}
	}
}

func TestRenderUnknownPage(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := Render(rec, "nope", nil); err == nil {
		t.Fatal("expected error for unknown page")
	}
}

func TestStaticServesCSS(t *testing.T) {
	srv := httptest.NewServer(Static())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), ":root") {
		t.Fatalf("unexpected css start: %q", buf[:n])
	}
}
