package githubcli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

// captureGit replaces gitRun with a recorder for the duration of a test, restoring it on
// cleanup, and returns pointers to the captured git args and env.
//
// @arg t The test handle (registers the cleanup).
// @return *[]string A pointer to the captured git arguments.
// @return *[]string A pointer to the captured extra environment.
//
// @testcase TestGitCloneCommandRunsGit inspects the captured args and env.
func captureGit(t *testing.T) (*[]string, *[]string) {
	t.Helper()
	var args, env []string
	orig := gitRun
	gitRun = func(ctx context.Context, a, e []string, out io.Writer) error {
		args, env = a, e
		return nil
	}
	t.Cleanup(func() { gitRun = orig })
	return &args, &env
}

// TestGitCloneCommandRunsGit checks the clone command builds the proxy URL, passes the
// destination directory, and supplies the subject token through the environment.
func TestGitCloneCommandRunsGit(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no default token file
	args, env := captureGit(t)

	root := NewRootCmd(&bytes.Buffer{})
	root.SetArgs([]string{"clone", "--repo", "clems4ever/granular",
		"--base-url", "http://localhost:8080", "--token", "TOK", "/tmp/dest"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	joined := strings.Join(*args, " ")
	if !strings.Contains(joined, "clone http://localhost:8080/git/clems4ever/granular.git /tmp/dest") {
		t.Fatalf("git args = %v", *args)
	}
	if !strings.Contains(joined, "credential.helper=") {
		t.Fatalf("git args missing credential helper: %v", *args)
	}
	found := false
	for _, e := range *env {
		if e == "GRANULAR_SUBJECT_TOKEN=TOK" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env missing token: %v", *env)
	}
}

// TestGitCloneNeedsToken checks the clone command errors clearly when no subject token is
// configured.
func TestGitCloneNeedsToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no default token file
	captureGit(t)

	root := NewRootCmd(&bytes.Buffer{})
	root.SetArgs([]string{"clone", "--repo", "clems4ever/granular", "--base-url", "http://localhost:8080"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "subject token") {
		t.Fatalf("err = %v, want a subject-token error", err)
	}
}

// TestGitPushArgsBuildsPush checks the push argument builder targets the proxy URL from the
// given directory, defaulting to HEAD when no refspec is given.
func TestGitPushArgsBuildsPush(t *testing.T) {
	got := pushArgs("/work", []string{"main"})("http://h/git/o/r.git", "")
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-C /work push http://h/git/o/r.git main") {
		t.Fatalf("push args = %v", got)
	}
	def := pushArgs(".", nil)("http://h/git/o/r.git", "")
	if def[len(def)-1] != "HEAD" {
		t.Fatalf("default refspec = %q, want HEAD", def[len(def)-1])
	}
}
