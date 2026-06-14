package cli

import "github.com/spf13/cobra"

// newGithubCmd builds the "github" command grouping GitHub operations.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The github command with its sub-commands.
//
// @testcase TestRootCommandTree verifies this command and its children are attached.
func newGithubCmd(server *string) *cobra.Command {
	cmd := &cobra.Command{Use: "github", Short: "GitHub operations"}
	cmd.AddCommand(
		newCloneCmd(server),
		newIssueCmd(server),
	)
	return cmd
}
