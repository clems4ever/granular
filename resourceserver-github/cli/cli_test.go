package githubcli

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
)

// find returns the immediate subcommand of parent named name, or nil.
func find(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// TestNewRootCmdHasGitHubCommands checks the root carries the built-in catalog
// and sign commands plus the GitHub operation commands, including nested groups.
func TestNewRootCmdHasGitHubCommands(t *testing.T) {
	root := NewRootCmd(&bytes.Buffer{})

	for _, name := range []string{"catalog", "sign", "clone", "push", "issue", "pull"} {
		if find(root, name) == nil {
			t.Errorf("missing top-level command %q", name)
		}
	}
	issue := find(root, "issue")
	if issue == nil {
		t.Fatal("issue group missing")
	}
	for _, name := range []string{"list", "view", "create", "comment", "edit", "close", "reopen"} {
		if find(issue, name) == nil {
			t.Errorf("missing issue subcommand %q", name)
		}
	}
	// The create command must require its mandatory flags.
	create := find(issue, "create")
	if create == nil {
		t.Fatal("issue create missing")
	}
	if create.Flag("repo") == nil || create.Flag("title") == nil {
		t.Error("issue create missing repo/title flags")
	}
}
