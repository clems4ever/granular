package web

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRenderProducesHTML renders a known page against the layout and checks the page data
// and the layout chrome both appear in the output.
func TestRenderProducesHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	err := Render(rec, "result", Nav{User: "alice@example.com", AuthEnabled: true},
		map[string]any{"Status": "approved", "Message": "All done"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "All done") || !strings.Contains(out, "alice@example.com") {
		t.Fatalf("rendered output missing expected content:\n%s", out)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
}

// TestRenderUnknownPage returns an fs.ErrNotExist-compatible error for an unknown page.
func TestRenderUnknownPage(t *testing.T) {
	if err := Render(httptest.NewRecorder(), "does-not-exist", Nav{}, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Render unknown page err = %v, want fs.ErrNotExist", err)
	}
}

// TestStaticServesCSS serves the embedded stylesheet through the static handler.
func TestStaticServesCSS(t *testing.T) {
	ts := httptest.NewServer(Static())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/static/style.css")
	if err != nil {
		t.Fatalf("get css: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("empty stylesheet")
	}
}
