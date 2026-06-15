package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/internal/api"
	"github.com/clems4ever/granular/internal/client"
)

// newRequestCmd builds "granular request", which submits a custom permissions
// request (a scoped capability bundle) for human approval.
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The request command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newRequestCmd(server *string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Request a custom set of permissions (read the schema from `granular catalog`)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readInput(file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var req api.GrantRequest
			if err := json.Unmarshal(data, &req); err != nil {
				return fmt.Errorf("invalid grant request JSON: %w", err)
			}
			return runRequest(cmd.Context(), client.New(*server), req, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "-", "grant request JSON file (\"-\" for stdin)")
	return cmd
}

// newCatalogCmd builds "granular catalog", which prints the server's capability
// manifest (the vocabulary for building a grant request).
//
// @arg server Pointer to the resolved --server flag value.
// @return *cobra.Command The catalog command.
//
// @testcase TestRootCommandTree reaches this command through the tree.
func newCatalogCmd(server *string) *cobra.Command {
	return &cobra.Command{
		Use:   "catalog",
		Short: "Print the server capability manifest (resources, actions, request schema)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			manifest, err := client.New(*server).Catalog(cmd.Context())
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(append(manifest, '\n'))
			return err
		},
	}
}

// runRequest submits a capability grant request and reports the approval URL.
//
// @arg ctx Context for cancellation.
// @arg c The HTTP client to the granular server.
// @arg req The grant request (capability bundle) to submit.
// @arg out The writer for user-facing output.
// @error error when the submission fails.
//
// @testcase TestRunRequestPrintsURL prints the approval URL.
func runRequest(ctx context.Context, c *client.Client, req api.GrantRequest, out io.Writer) error {
	resp, err := c.RequestGrant(ctx, req)
	if err != nil {
		return err
	}
	if resp.Status != api.StatusPending {
		return fmt.Errorf("unexpected status %s: %s", resp.Status, resp.Error)
	}
	fmt.Fprintf(out, "Grant requested. Open this URL to review and approve:\n\n  %s\n", resp.ApprovalURL)
	return nil
}

// readInput reads the request body from a file, or stdin when the path is "-".
//
// @arg file The file path, or "-" for stdin.
// @arg stdin The reader used when file is "-".
// @return []byte The read bytes.
// @error error when the file cannot be read.
//
// @testcase TestRunRequestPrintsURL supplies the request via a reader.
func readInput(file string, stdin io.Reader) ([]byte, error) {
	if file == "" || file == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(file)
}
