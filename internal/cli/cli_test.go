package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

func TestRootCommandTree(t *testing.T) {
	root := NewRootCmd()
	for _, path := range [][]string{
		{"github"},
		{"github", "clone"},
		{"github", "issue"},
		{"github", "issue", "list"},
	} {
		cmd, _, err := root.Find(path)
		if err != nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("command %v not found: %v", path, err)
		}
	}
}

// fixedServer returns a server that always responds with body.
func fixedServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestRunClonePendingPrintsURL(t *testing.T) {
	ts := fixedServer(t, http.StatusAccepted, `{"status":"pending","request_id":"req1","approval_url":"http://x/approve/req1"}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.clone", Params: map[string]any{"repo": "a/b"}}
	if err := runClone(context.Background(), client.New(ts.URL), req, "/tmp/dest", "", &out); err != nil {
		t.Fatalf("runClone: %v", err)
	}
	if !strings.Contains(out.String(), "http://x/approve/req1") || !strings.Contains(out.String(), "re-run") {
		t.Fatalf("expected approval URL and re-run hint, got: %q", out.String())
	}
}

func TestRunCloneClonesViaProxy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// Build a local source repository with one commit; git can clone from a path.
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", ".")
	runGit(t, src, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")

	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"clone_url":"`+src+`","repo":"a/b"}}`)
	dest := filepath.Join(t.TempDir(), "dest")
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.clone", Params: map[string]any{"repo": "a/b"}}
	if err := runClone(context.Background(), client.New(ts.URL), req, dest, "", &out); err != nil {
		t.Fatalf("runClone: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("clone did not produce README.md: %v", err)
	}
}

func TestRunIssueListPendingPrintsURL(t *testing.T) {
	ts := fixedServer(t, http.StatusAccepted, `{"status":"pending","request_id":"req1","approval_url":"http://x/approve/req1"}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.list", Params: map[string]any{"repo": "a/b"}}
	if err := runIssueList(context.Background(), client.New(ts.URL), req, &out); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if !strings.Contains(out.String(), "http://x/approve/req1") || !strings.Contains(out.String(), "list the issues") {
		t.Fatalf("expected approval URL and hint, got: %q", out.String())
	}
}

func TestRunIssueListPrintsIssues(t *testing.T) {
	body := `{"status":"completed","result":{"issues":[{"number":7,"title":"Fix the bug","state":"open","author":"octocat"}]}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.list", Params: map[string]any{"repo": "a/b"}}
	if err := runIssueList(context.Background(), client.New(ts.URL), req, &out); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#7") || !strings.Contains(got, "Fix the bug") || !strings.Contains(got, "octocat") {
		t.Fatalf("issue line not rendered: %q", got)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
