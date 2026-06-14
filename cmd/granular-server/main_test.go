package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvOrFallback(t *testing.T) {
	const key = "GRANULAR_TEST_ENVOR"
	_ = os.Unsetenv(key)
	if got := envOr(key, "fallback"); got != "fallback" {
		t.Fatalf("unset variable should use fallback, got %q", got)
	}
	t.Setenv(key, "value")
	if got := envOr(key, "fallback"); got != "value" {
		t.Fatalf("set variable should win, got %q", got)
	}
}

func TestRunRejectsBadWorkspace(t *testing.T) {
	// Point the workspace at a path whose parent is a regular file, so the
	// MkdirAll in run fails fast before any server is started.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRANULAR_WORKSPACE", filepath.Join(file, "workspace"))
	if err := run(); err == nil {
		t.Fatal("run should fail when the workspace cannot be created")
	}
}

// TestMainIsEntryPoint is a placeholder: main only wires log.Fatal to run and
// cannot be invoked in-process without terminating the test binary, so the
// delegation it performs is exercised through TestRunRejectsBadWorkspace.
func TestMainIsEntryPoint(t *testing.T) {
	if testing.Short() {
		t.Skip("main is an entry point exercised via run")
	}
}
