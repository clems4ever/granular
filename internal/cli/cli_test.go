package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
		{"github", "issue", "view"},
		{"github", "issue", "comment"},
		{"github", "issue", "create"},
		{"github", "issue", "edit"},
		{"github", "issue", "close"},
		{"github", "issue", "reopen"},
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
	if err := runIssueList(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	if !strings.Contains(out.String(), "http://x/approve/req1") || !strings.Contains(out.String(), "list the issues") {
		t.Fatalf("expected approval URL and hint, got: %q", out.String())
	}
}

func TestRunIssueListPrintsIssues(t *testing.T) {
	body := `{"status":"completed","result":{"issues":[{"number":7,"title":"Fix the bug","state":"open","user":{"login":"octocat"}}]}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.list", Params: map[string]any{"repo": "a/b"}}
	if err := runIssueList(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#7") || !strings.Contains(got, "Fix the bug") || !strings.Contains(got, "octocat") {
		t.Fatalf("issue line not rendered: %q", got)
	}
}

func TestRunIssueViewPendingPrintsURL(t *testing.T) {
	ts := fixedServer(t, http.StatusAccepted, `{"status":"pending","request_id":"req1","approval_url":"http://x/approve/req1"}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.view", Params: map[string]any{"repo": "a/b", "number": 7}}
	if err := runIssueView(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueView: %v", err)
	}
	if !strings.Contains(out.String(), "http://x/approve/req1") || !strings.Contains(out.String(), "view the issue") {
		t.Fatalf("expected approval URL and hint, got: %q", out.String())
	}
}

func TestRunIssueViewPrintsIssue(t *testing.T) {
	body := `{"status":"completed","result":{"number":7,"title":"the title","state":"open","user":{"login":"octocat"},"labels":[{"name":"bug"}],"comments":2,"body":"the body","html_url":"u"}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.view", Params: map[string]any{"repo": "a/b", "number": 7}}
	if err := runIssueView(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueView: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#7") || !strings.Contains(got, "the title") || !strings.Contains(got, "the body") || !strings.Contains(got, "octocat") || !strings.Contains(got, "bug") {
		t.Fatalf("issue details not rendered: %q", got)
	}
}

func TestRunIssueListJSON(t *testing.T) {
	body := `{"status":"completed","result":{"issues":[{"number":7,"title":"Fix the bug","state":"open","author":"octocat"}]}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.list", Params: map[string]any{"repo": "a/b"}}
	if err := runIssueList(context.Background(), client.New(ts.URL), req, &out, true); err != nil {
		t.Fatalf("runIssueList: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out.String())
	}
	if len(decoded) != 1 || decoded[0]["title"] != "Fix the bug" {
		t.Fatalf("unexpected JSON: %s", out.String())
	}
}

func TestRunIssueViewJSON(t *testing.T) {
	body := `{"status":"completed","result":{"number":7,"title":"the title","state":"open"}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.view", Params: map[string]any{"repo": "a/b", "number": 7}}
	if err := runIssueView(context.Background(), client.New(ts.URL), req, &out, true); err != nil {
		t.Fatalf("runIssueView: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, out.String())
	}
	if decoded["title"] != "the title" {
		t.Fatalf("unexpected JSON: %s", out.String())
	}
}

func TestRunIssueViewPrintsComments(t *testing.T) {
	body := `{"status":"completed","result":{"number":7,"title":"t","state":"open","user":{"login":"octocat"},"comments_list":[{"body":"a comment","user":{"login":"alice"}}]}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.view", Params: map[string]any{"repo": "a/b", "number": 7, "comments": true}}
	if err := runIssueView(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueView: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "a comment") || !strings.Contains(got, "alice wrote") {
		t.Fatalf("comments not rendered: %q", got)
	}
}

func TestRunIssueCommentPendingPrintsURL(t *testing.T) {
	ts := fixedServer(t, http.StatusAccepted, `{"status":"pending","request_id":"req1","approval_url":"http://x/approve/req1"}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.comment", Params: map[string]any{"repo": "a/b", "number": 1, "body": "hi"}}
	if err := runIssueComment(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueComment: %v", err)
	}
	if !strings.Contains(out.String(), "http://x/approve/req1") || !strings.Contains(out.String(), "post the comment") {
		t.Fatalf("expected approval URL and hint, got: %q", out.String())
	}
}

func TestRunIssueCommentReportsResult(t *testing.T) {
	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"html_url":"http://gh/c/99"}}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.comment", Params: map[string]any{"repo": "a/b", "number": 1, "body": "hi"}}
	if err := runIssueComment(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueComment: %v", err)
	}
	if !strings.Contains(out.String(), "http://gh/c/99") {
		t.Fatalf("expected comment URL, got: %q", out.String())
	}
}

func TestRunIssueCreateReportsResult(t *testing.T) {
	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"number":42,"html_url":"http://gh/i/42"}}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.create", Params: map[string]any{"repo": "a/b", "title": "t"}}
	if err := runIssueCreate(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runIssueCreate: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#42") || !strings.Contains(got, "http://gh/i/42") {
		t.Fatalf("expected issue number and URL, got: %q", got)
	}
}

func TestRunIssueActionReportsResult(t *testing.T) {
	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"number":5,"html_url":"http://gh/i/5"}}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.close", Params: map[string]any{"repo": "a/b", "number": 5}}
	if err := runIssueAction(context.Background(), client.New(ts.URL), req, "close the issue", "closed", &out, false); err != nil {
		t.Fatalf("runIssueAction: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#5") || !strings.Contains(got, "closed") || !strings.Contains(got, "http://gh/i/5") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRunIssueActionPending(t *testing.T) {
	ts := fixedServer(t, http.StatusAccepted, `{"status":"pending","request_id":"r","approval_url":"http://x/approve/r"}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.issue.reopen", Params: map[string]any{"repo": "a/b", "number": 5}}
	if err := runIssueAction(context.Background(), client.New(ts.URL), req, "reopen the issue", "reopened", &out, false); err != nil {
		t.Fatalf("runIssueAction: %v", err)
	}
	if !strings.Contains(out.String(), "reopen the issue") {
		t.Fatalf("expected pending hint, got: %q", out.String())
	}
}

func TestResolveBodyFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.txt")
	if err := os.WriteFile(path, []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBody("inline", path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from file" {
		t.Fatalf("body-file should win: %q", got)
	}
	if got, _ := resolveBody("inline", "", nil); got != "inline" {
		t.Fatalf("inline body expected, got %q", got)
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
