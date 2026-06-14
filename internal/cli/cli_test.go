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
		{"github", "push"},
		{"github", "issue"},
		{"github", "issue", "list"},
		{"github", "issue", "view"},
		{"github", "issue", "comment"},
		{"github", "issue", "create"},
		{"github", "issue", "edit"},
		{"github", "issue", "close"},
		{"github", "issue", "reopen"},
		{"github", "pr"},
		{"github", "pr", "list"},
		{"github", "pr", "view"},
		{"github", "pr", "diff"},
		{"github", "pr", "create"},
		{"github", "pr", "comment"},
		{"github", "pr", "review"},
		{"github", "pr", "edit"},
		{"github", "pr", "merge"},
		{"github", "pr", "close"},
		{"github", "pr", "reopen"},
		{"request"},
		{"catalog"},
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

func TestRunRequestPrintsURL(t *testing.T) {
	ts := fixedServer(t, http.StatusAccepted, `{"status":"pending","request_id":"req1","approval_url":"http://x/approve/req1"}`)
	var out bytes.Buffer
	req := api.PermissionsRequest{Capabilities: []api.Capability{{Actions: []string{"issues.read"}, Resource: api.ResourceSelector{Type: "github.repo", Match: map[string]string{"owner": "o", "name": "n"}}}}}
	if err := runRequest(context.Background(), client.New(ts.URL), req, &out); err != nil {
		t.Fatalf("runRequest: %v", err)
	}
	if !strings.Contains(out.String(), "http://x/approve/req1") {
		t.Fatalf("expected approval URL, got: %q", out.String())
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

func TestRunPullListPrintsPulls(t *testing.T) {
	body := `{"status":"completed","result":{"pulls":[{"number":7,"title":"Add feature","state":"open","user":{"login":"octocat"}}]}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.pull.list", Params: map[string]any{"repo": "a/b"}}
	if err := runPullList(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runPullList: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#7") || !strings.Contains(got, "Add feature") || !strings.Contains(got, "octocat") {
		t.Fatalf("pull line not rendered: %q", got)
	}
}

func TestRunPullListJSON(t *testing.T) {
	body := `{"status":"completed","result":{"pulls":[{"number":7,"title":"Add feature"}]}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.pull.list", Params: map[string]any{"repo": "a/b"}}
	if err := runPullList(context.Background(), client.New(ts.URL), req, &out, true); err != nil {
		t.Fatalf("runPullList: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out.String())
	}
	if len(decoded) != 1 || decoded[0]["title"] != "Add feature" {
		t.Fatalf("unexpected JSON: %s", out.String())
	}
}

func TestRunPullViewPrintsPull(t *testing.T) {
	body := `{"status":"completed","result":{"number":7,"title":"the pr","state":"open","user":{"login":"octocat"},"head":{"ref":"feature"},"base":{"ref":"main"},"body":"the body","html_url":"u"}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.pull.view", Params: map[string]any{"repo": "a/b", "number": 7}}
	if err := runPullView(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runPullView: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "the pr") || !strings.Contains(got, "feature -> main") || !strings.Contains(got, "the body") {
		t.Fatalf("pull details not rendered: %q", got)
	}
}

func TestRunPullDiffPrintsDiff(t *testing.T) {
	body := `{"status":"completed","result":{"diff":"diff --git a/x b/x\n+new\n"}}`
	ts := fixedServer(t, http.StatusOK, body)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.pull.diff", Params: map[string]any{"repo": "a/b", "number": 7}}
	if err := runPullDiff(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runPullDiff: %v", err)
	}
	if !strings.Contains(out.String(), "diff --git a/x b/x") {
		t.Fatalf("diff not rendered: %q", out.String())
	}
}

func TestRunPullActionReportsResult(t *testing.T) {
	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"number":5,"html_url":"http://gh/pr/5"}}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.pull.merge", Params: map[string]any{"repo": "a/b", "number": 5}}
	if err := runPullAction(context.Background(), client.New(ts.URL), req, "merge the pull request", "merged", &out, false); err != nil {
		t.Fatalf("runPullAction: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#5") || !strings.Contains(got, "merged") || !strings.Contains(got, "http://gh/pr/5") {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRunPullCommentReportsResult(t *testing.T) {
	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"html_url":"http://gh/c/1"}}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.pull.comment", Params: map[string]any{"repo": "a/b", "number": 1, "body": "hi"}}
	if err := runPullComment(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runPullComment: %v", err)
	}
	if !strings.Contains(out.String(), "http://gh/c/1") {
		t.Fatalf("expected comment URL, got: %q", out.String())
	}
}

func TestRunPullCreateReportsResult(t *testing.T) {
	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"number":42,"html_url":"http://gh/pr/42"}}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.pull.create", Params: map[string]any{"repo": "a/b", "title": "t", "head": "f", "base": "m"}}
	if err := runPullCreate(context.Background(), client.New(ts.URL), req, &out, false); err != nil {
		t.Fatalf("runPullCreate: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "#42") || !strings.Contains(got, "http://gh/pr/42") {
		t.Fatalf("expected pull request number and URL, got: %q", got)
	}
}

func TestRunPushPendingPrintsURL(t *testing.T) {
	ts := fixedServer(t, http.StatusAccepted, `{"status":"pending","request_id":"req1","approval_url":"http://x/approve/req1"}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.push", Params: map[string]any{"repo": "a/b"}}
	if err := runPush(context.Background(), client.New(ts.URL), req, "/tmp/dir", "", &out); err != nil {
		t.Fatalf("runPush: %v", err)
	}
	if !strings.Contains(out.String(), "http://x/approve/req1") || !strings.Contains(out.String(), "perform the push") {
		t.Fatalf("expected approval URL and hint, got: %q", out.String())
	}
}

func TestRunPushPushesViaProxy(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// A bare remote stands in for the proxy endpoint.
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, t.TempDir(), "init", "--bare", remote)

	// A local repository with one commit on branch main.
	local := filepath.Join(t.TempDir(), "local")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, local, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(local, "f.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, local, "add", ".")
	runGit(t, local, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")

	ts := fixedServer(t, http.StatusOK, `{"status":"completed","result":{"push_url":"`+remote+`","repo":"a/b"}}`)
	var out bytes.Buffer
	req := api.OperationRequest{Type: "github.push", Params: map[string]any{"repo": "a/b"}}
	if err := runPush(context.Background(), client.New(ts.URL), req, local, "", &out); err != nil {
		t.Fatalf("runPush: %v\n%s", err, out.String())
	}
	// The branch must now exist in the remote.
	if out := exec.Command("git", "-C", remote, "rev-parse", "--verify", "main").Run(); out != nil {
		t.Fatalf("push did not create main in the remote")
	}
}
