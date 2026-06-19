// Package clientcli implements the granular client command tree on top of the client
// SDK. It reads a YAML configuration listing the authorization server and the known
// resource servers, then exposes commands to catalog the resource servers' permission schemas, run
// operations (executed when authorized, a clear error otherwise), sign a grant request
// against a resource server, and pack one or more signed grant requests into a proposal submitted
// to the AS for human approval. The cmd/granular-client binary is a thin entrypoint that
// delegates here.
//
// Subject lifecycle (minting and destroying the subject token) is an administrative concern
// handled by a separate command (granular-subject); this CLI only uses a configured token.
package clientcli

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/spf13/cobra"

	"github.com/clems4ever/granular/client"
)

// app holds the CLI's shared state: the resolved configuration flags, the output writer,
// and the SDK client built once configuration is loaded.
type app struct {
	configPath string
	token      string
	out        io.Writer
	c          *client.Client
}

// NewRootCmd builds the root "granular" command, wiring the shared flags, the
// configuration loading, and the sub-command tree.
//
// @arg out The writer the commands print user-facing output to.
// @return *cobra.Command The configured root command.
//
// @testcase TestCommandTree checks the sub-commands are wired.
func NewRootCmd(out io.Writer) *cobra.Command {
	a := &app{out: out}
	root := &cobra.Command{
		Use:               "granular",
		Short:             "Catalog, run, and request human-approved operations across granular resource servers",
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: func(*cobra.Command, []string) error { return a.load() },
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", "granular-client.yaml", "path to the YAML configuration file")
	root.PersistentFlags().StringVar(&a.token, "token", "", "subject token (overrides the configured token_file)")
	root.AddCommand(a.catalogCmd(), a.templateCmd(), a.opCmd(), a.signCmd(), a.proposeCmd(), a.grantsCmd(), a.revokeCmd())
	return root
}

// load resolves the configuration (falling back to defaults when the file is absent) and
// builds the SDK client.
//
// @error error when the configuration file exists but cannot be read or parsed.
//
// @testcase TestCommandTree triggers configuration loading via the root command.
func (a *app) load() error {
	cfg := Default()
	if _, err := os.Stat(a.configPath); !errors.Is(err, fs.ErrNotExist) {
		loaded, err := Load(a.configPath)
		if err != nil {
			return err
		}
		cfg = loaded
	}
	a.c = cfg.toClient(a.token)
	return nil
}

// catalogCmd builds the "catalog" command: print the permission schema of the resource servers
// (optionally a named subset).
//
// @return *cobra.Command The catalog command.
//
// @testcase TestCommandTree checks the catalog command is present.
func (a *app) catalogCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "catalog [resource-server-id ...]",
		Short: "Print the permission schema of the resource servers (resources, actions, example)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCatalog(context.Background(), a.c, args, asJSON, a.out)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw schema as JSON for programmatic use")
	return cmd
}

// templateCmd builds the "template" command: list grant templates, or detail what a
// named template grants.
//
// @return *cobra.Command The template command.
//
// @testcase TestCommandTree checks the template command is present.
func (a *app) templateCmd() *cobra.Command {
	var resourceServerID string
	cmd := &cobra.Command{
		Use:   "template [name]",
		Short: "Explore grant templates (list, or show what a named template grants)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var ids []string
			if resourceServerID != "" {
				ids = []string{resourceServerID}
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runTemplate(context.Background(), a.c, ids, name, a.out)
		},
	}
	cmd.Flags().StringVar(&resourceServerID, "resource-server", "", "limit to one resource server")
	return cmd
}

// opCmd builds the "op" command: run an operation on a resource server.
//
// @return *cobra.Command The op command.
//
// @testcase TestCommandTree checks the op command is present.
func (a *app) opCmd() *cobra.Command {
	var params []string
	cmd := &cobra.Command{
		Use:   "op <resource-server-id> <type>",
		Short: "Run an operation on a resource server (executes when authorized)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := parseParams(params)
			if err != nil {
				return err
			}
			return runOp(context.Background(), a.c, args[0], args[1], p, a.out)
		},
	}
	cmd.Flags().StringArrayVarP(&params, "param", "p", nil, "operation parameter, key=value (repeatable)")
	return cmd
}

// signCmd builds the "sign" command: build a grant request and have one resource server sign it,
// storing the signed result for later submission.
//
// @return *cobra.Command The sign command.
//
// @testcase TestCommandTree checks the sign command is present.
func (a *app) signCmd() *cobra.Command {
	var resourceServerID, reason, resType, match, out, template string
	var actions, binds []string
	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Build a grant request (freeform or from a template) and have a resource server sign it",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildSignRequest(template, binds, reason, actions, resType, match)
			if err != nil {
				return err
			}
			return runSign(context.Background(), a.c, resourceServerID, req, out, a.out)
		},
	}
	cmd.Flags().StringVar(&resourceServerID, "resource-server", "", "resource server id to sign against (required)")
	cmd.Flags().StringVar(&out, "out", "", "file to write the signed request to (default stdout)")
	// Template form.
	cmd.Flags().StringVar(&template, "template", "", "instantiate a resource server template instead of a freeform capability")
	cmd.Flags().StringArrayVar(&binds, "bind", nil, "template parameter, name=value (repeatable)")
	// Freeform form.
	cmd.Flags().StringVar(&reason, "reason", "", "human-readable reason shown on the consent screen")
	cmd.Flags().StringSliceVar(&actions, "actions", nil, "action or group names to request")
	cmd.Flags().StringVar(&resType, "resource", "", "resource type the capability is scoped to")
	cmd.Flags().StringVar(&match, "match", "", "resource match fields, key=value,key=value")
	_ = cmd.MarkFlagRequired("resource-server")
	return cmd
}

// grantsCmd builds the "grants" command: list the grants currently attached to the
// configured subject token, letting an agent introspect what it holds.
//
// @return *cobra.Command The grants command.
//
// @testcase TestCommandTree checks the grants command is present.
func (a *app) grantsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "grants",
		Short: "List the grants currently attached to your subject token",
		RunE: func(*cobra.Command, []string) error {
			return runGrants(context.Background(), a.c, a.out)
		},
	}
}

// revokeCmd builds the "revoke" command: revoke every active grant attached to the
// configured subject token in one step, letting an agent drop all the authority it holds.
//
// @return *cobra.Command The revoke command.
//
// @testcase TestCommandTree checks the revoke command is present.
func (a *app) revokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke",
		Short: "Revoke all the grants currently attached to your subject token",
		RunE: func(*cobra.Command, []string) error {
			return runRevokeGrants(context.Background(), a.c, a.out)
		},
	}
}

// proposeCmd builds the "propose" command: pack one or more signed grant requests into a
// proposal and submit it to the AS for human approval.
//
// @return *cobra.Command The propose command.
//
// @testcase TestCommandTree checks the propose command is present.
func (a *app) proposeCmd() *cobra.Command {
	var approver, reason string
	cmd := &cobra.Command{
		Use:   "propose <signed-request-file ...>",
		Short: "Pack signed grant requests and submit them to the AS for approval",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPropose(context.Background(), a.c, approver, reason, args, a.out)
		},
	}
	cmd.Flags().StringVar(&approver, "approver", "", "email of the human who must approve (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "why these permissions are needed; shown to the approver on the consent screen (optional but recommended)")
	_ = cmd.MarkFlagRequired("approver")
	return cmd
}
