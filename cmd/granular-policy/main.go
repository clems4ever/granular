// Command granular-policy is the administrative CLI for policy lifecycle on the
// authorization server (AS). Policy management is independent from the grant management
// the granular client CLI performs: an administrator mints a policy token here, hands it
// to a client (which submits proposals and runs operations under it), and can inspect or
// destroy it. It is a thin implementation of the client SDK's policy methods.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/client"
)

// admin holds the policy CLI's shared state: the AS URL, the token flags, and the writer.
type admin struct {
	asURL     string
	token     string
	tokenFile string
	out       io.Writer
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
	root.PersistentFlags().StringVar(&a.token, "token", "", "policy token (for show/destroy)")
	root.PersistentFlags().StringVar(&a.tokenFile, "token-file", "", "file holding the policy token (for show/destroy)")
	root.AddCommand(a.createCmd(), a.showCmd(), a.destroyCmd())
	return root
}

// client builds an SDK client for policy operations, resolving the token from the
// --token flag or the --token-file.
//
// @return *client.Client The configured client.
// @error error when the token file is set but cannot be read.
//
// @testcase TestRunPolicy builds a client for the policy operations.
func (a *admin) client() (*client.Client, error) {
	token := a.token
	if token == "" && a.tokenFile != "" {
		data, err := os.ReadFile(a.tokenFile)
		if err != nil {
			return nil, fmt.Errorf("token-file: %w", err)
		}
		token = strings.TrimSpace(string(data))
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
		RunE:  func(*cobra.Command, []string) error { return a.run(runCreate) },
	}
}

// showCmd builds the "show" command: list the grants attached to the policy token.
//
// @return *cobra.Command The show command.
//
// @testcase TestCommandTree checks the show command is present.
func (a *admin) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the grants attached to the policy token",
		RunE:  func(*cobra.Command, []string) error { return a.run(runShow) },
	}
}

// destroyCmd builds the "destroy" command: destroy the policy token and its grants.
//
// @return *cobra.Command The destroy command.
//
// @testcase TestCommandTree checks the destroy command is present.
func (a *admin) destroyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "destroy",
		Short: "Destroy the policy token and its grants",
		RunE:  func(*cobra.Command, []string) error { return a.run(runDestroy) },
	}
}

// run builds the client and invokes a policy action with it and the output writer.
//
// @arg action The policy action to run with the client.
// @error error when the client cannot be built or the action fails.
//
// @testcase TestRunPolicy runs the create/show/destroy actions.
func (a *admin) run(action func(context.Context, *client.Client, io.Writer) error) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	return action(context.Background(), c, a.out)
}

// runCreate mints a policy token and prints it for the administrator to distribute.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
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

// runShow lists the active grants attached to the policy token.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg w The writer for output.
// @error ErrNoToken when no token is configured.
// @error error when the AS call fails.
//
// @testcase TestRunPolicy lists grants.
func runShow(ctx context.Context, c *client.Client, w io.Writer) error {
	if c.Token() == "" {
		return errors.New("a token is required (set --token or --token-file)")
	}
	grants, err := c.Policy(ctx)
	if err != nil {
		return err
	}
	if len(grants) == 0 {
		fmt.Fprintln(w, "no active grants")
		return nil
	}
	for _, g := range grants {
		fmt.Fprintf(w, "%s (expires %s): %s\n", g.GatewayID, g.ExpiresAt, g.Item.Presentation.Summary)
	}
	return nil
}

// runDestroy destroys the policy token and prints how many grants were removed.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg w The writer for output.
// @error error when no token is configured or the AS call fails.
//
// @testcase TestRunPolicy destroys the policy.
func runDestroy(ctx context.Context, c *client.Client, w io.Writer) error {
	if c.Token() == "" {
		return errors.New("a token is required (set --token or --token-file)")
	}
	n, err := c.DestroyPolicy(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "destroyed %d grant(s)\n", n)
	return nil
}
