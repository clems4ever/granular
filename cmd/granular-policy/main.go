// Command granular-policy is the administrative CLI for policy lifecycle on the
// authorization server (AS). Policy management is independent from the grant management
// the granular client CLI performs: an administrator authenticates with the AS admin
// token, mints a policy token here, hands it to a client (which submits proposals and
// runs operations under it), and can inspect or destroy it. It is a thin implementation
// of the client SDK's policy methods.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/client"
)

// admin holds the policy CLI's shared state: the AS URL, the admin-token flags, and the
// output writer.
type admin struct {
	asURL          string
	adminToken     string
	adminTokenFile string
	out            io.Writer
}

// main builds the command tree and executes it, exiting non-zero on error.
//
// @testcase TestMainIsEntryPoint is a placeholder; main only builds and executes the tree.
func main() {
	if err := newRootCmd(os.Stdout).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// newRootCmd builds the root "granular-policy" command with the shared flags and the
// create/show/destroy sub-commands.
//
// @arg out The writer the commands print to.
// @return *cobra.Command The configured root command.
//
// @testcase TestCommandTree checks the sub-commands are wired.
func newRootCmd(out io.Writer) *cobra.Command {
	a := &admin{out: out}
	root := &cobra.Command{
		Use:           "granular-policy",
		Short:         "Administer policy tokens on the authorization server",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&a.asURL, "as", "http://localhost:9090", "authorization server base URL")
	root.PersistentFlags().StringVar(&a.adminToken, "admin-token", "", "AS admin token (gates policy administration)")
	root.PersistentFlags().StringVar(&a.adminTokenFile, "admin-token-file", "", "file holding the AS admin token")
	root.AddCommand(a.createCmd(), a.showCmd(), a.destroyCmd())
	return root
}

// client builds an SDK client authenticated with the resolved admin token (from the
// --admin-token flag or the --admin-token-file), requiring one to be set.
//
// @return *client.Client The configured client.
// @error error when no admin token is set or its file cannot be read.
//
// @testcase TestRunPolicy builds a client for the policy operations.
func (a *admin) client() (*client.Client, error) {
	token := a.adminToken
	if token == "" && a.adminTokenFile != "" {
		data, err := os.ReadFile(a.adminTokenFile)
		if err != nil {
			return nil, fmt.Errorf("admin-token-file: %w", err)
		}
		token = strings.TrimSpace(string(data))
	}
	if token == "" {
		return nil, fmt.Errorf("an admin token is required (set --admin-token or --admin-token-file)")
	}
	return client.New(client.Config{ASURL: a.asURL, Token: token}), nil
}

// createCmd builds the "create" command: mint a new policy token and print it.
//
// @return *cobra.Command The create command.
//
// @testcase TestCommandTree checks the create command is present.
func (a *admin) createCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create",
		Short: "Mint a new policy token",
		RunE: func(*cobra.Command, []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			return runCreate(context.Background(), c, a.out)
		},
	}
}

// showCmd builds the "show" command: list the grants attached to a policy token.
//
// @return *cobra.Command The show command.
//
// @testcase TestCommandTree checks the show command is present.
func (a *admin) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <policy-token>",
		Short: "Show the grants attached to a policy token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			return runShow(context.Background(), c, args[0], a.out)
		},
	}
}

// destroyCmd builds the "destroy" command: destroy a policy token and its grants.
//
// @return *cobra.Command The destroy command.
//
// @testcase TestCommandTree checks the destroy command is present.
func (a *admin) destroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "destroy <policy-token>",
		Short: "Destroy a policy token and its grants",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			return runDestroy(context.Background(), c, args[0], a.out)
		},
	}
}

// runCreate mints a policy token and prints it for the administrator to distribute.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK (authenticated with the admin token).
// @arg w The writer for output.
// @error error when the AS call fails.
//
// @testcase TestRunPolicy creates a token.
func runCreate(ctx context.Context, c *client.Client, w io.Writer) error {
	tok, err := c.CreatePolicy(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%s\n", tok)
	return nil
}

// runShow lists the active grants attached to a policy token.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK (authenticated with the admin token).
// @arg policyToken The policy token to inspect.
// @arg w The writer for output.
// @error error when the AS call fails.
//
// @testcase TestRunPolicy lists grants.
func runShow(ctx context.Context, c *client.Client, policyToken string, w io.Writer) error {
	grants, err := c.Policy(ctx, policyToken)
	if err != nil {
		return err
	}
	if len(grants) == 0 {
		fmt.Fprintln(w, "no active grants")
		return nil
	}
	for _, g := range grants {
		fmt.Fprintf(w, "%s (expires %s): %s\n", g.ResourceServerID, g.ExpiresAt, g.Item.Presentation.Summary)
	}
	return nil
}

// runDestroy destroys a policy token and prints how many grants were removed.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK (authenticated with the admin token).
// @arg policyToken The policy token to destroy.
// @arg w The writer for output.
// @error error when the AS call fails.
//
// @testcase TestRunPolicy destroys the policy.
func runDestroy(ctx context.Context, c *client.Client, policyToken string, w io.Writer) error {
	n, err := c.DestroyPolicy(ctx, policyToken)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "destroyed %d grant(s)\n", n)
	return nil
}
