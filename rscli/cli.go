package rscli

import (
	"context"
	"io"
	"strings"

	"github.com/clems4ever/granular/client"
	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/resourceserver"
	"github.com/spf13/cobra"
)

// FlagType is the value type of an operation command flag. It decides both the
// cobra flag kind and how the value is encoded into the operation's parameters:
// strings/ints/bools/string-slices are sent as their native JSON types, which is
// what resource servers parse (a string-only encoding can't set bool or list
// params).
type FlagType int

const (
	// StringFlag is a string-valued flag/param.
	StringFlag FlagType = iota
	// IntFlag is an integer-valued flag/param.
	IntFlag
	// BoolFlag is a boolean-valued flag/param.
	BoolFlag
	// StringSliceFlag is a repeatable string flag, sent as a JSON array param.
	StringSliceFlag
)

// Flag declares one flag of an operation command and the operation parameter it
// fills. An optional flag is sent only when the user set it, so it never clobbers
// a resource-server-side default (e.g. a list's default page size).
type Flag struct {
	// Name is the flag name (e.g. "title"); it is also the parameter name unless
	// Param overrides it.
	Name string
	// Param is the operation parameter name when it differs from Name.
	Param string
	// Type is the value type; the zero value is StringFlag.
	Type FlagType
	// Required marks the flag as mandatory.
	Required bool
	// Usage is the flag's help text.
	Usage string
}

// param returns the operation parameter name this flag fills (Param, or Name).
//
// @return string The parameter name.
//
// @testcase TestOperationCommandSendsTypedParams checks a flag fills its param.
func (f Flag) param() string {
	if f.Param != "" {
		return f.Param
	}
	return f.Name
}

// OpCommand declares one operation as a CLI sub-command. Path is the (possibly
// multi-word) command path under the root, e.g. "issue create" nests a "create"
// command under an "issue" group. Type is the resource server operation type id.
type OpCommand struct {
	// Path is the space-separated command path, e.g. "issue create".
	Path string
	// Type is the operation type id, e.g. "github.issue.create".
	Type string
	// Short is the one-line command description.
	Short string
	// Flags declares the command's flags and the params they fill.
	Flags []Flag
}

// Spec describes the resource-server CLI to build.
type Spec struct {
	// Use is the root command name (the binary name), e.g. "granular-github".
	Use string
	// Short is the root command's one-line description.
	Short string
	// RSID is the resource server id this CLI targets, e.g. "github".
	RSID string
	// DefaultBaseURL is the RS base URL used when --base-url is not given.
	DefaultBaseURL string
	// Operations are the RS-specific operation commands.
	Operations []OpCommand
	// Extra optionally contributes fully custom commands, given the shared App.
	Extra func(*App) []*cobra.Command
}

// App is the shared runtime handle passed to command handlers. Its client is
// constructed once configuration is loaded, before any command runs.
type App struct {
	// RSID is the resource server id this CLI targets.
	RSID string
	// Out is the writer command output is written to.
	Out io.Writer

	c              *client.Client
	defaultBaseURL string
	configPath     string
	baseURL        string
	token          string
	tokenFile      string
}

// Client returns the configured SDK client. It is non-nil for any command that
// runs after configuration is loaded.
//
// @return *client.Client The configured client.
//
// @testcase TestOperationCommandSendsTypedParams uses the client to run an operation.
func (a *App) Client() *client.Client { return a.c }

// Run executes an operation on the target resource server.
//
// @arg ctx Context for cancellation.
// @arg op The operation type and parameters.
// @return client.Result The executed operation's result.
// @error error from the client (including client.ErrNotAuthorized on denial).
//
// @testcase TestOperationCommandSendsTypedParams runs an operation and checks the result.
func (a *App) Run(ctx context.Context, op resourceserver.OperationRequest) (client.Result, error) {
	return a.c.Run(ctx, a.RSID, op)
}

// Sign asks the target resource server to sign a grant request.
//
// @arg ctx Context for cancellation.
// @arg req The grant request to sign.
// @return proposal.SignedGrantRequest The resource-server-signed grant request.
// @error error when the resource server rejects or signing fails.
//
// @testcase TestSignWritesSignedRequest signs a freeform request.
func (a *App) Sign(ctx context.Context, req resourceserver.GrantRequest) (proposal.SignedGrantRequest, error) {
	return a.c.Sign(ctx, a.RSID, req)
}

// Schema fetches the target resource server's permission schema.
//
// @arg ctx Context for cancellation.
// @return resourceserver.Schema The resource server's schema.
// @error error when the schema cannot be fetched.
//
// @testcase TestCatalogPrintsSchema fetches and prints the schema.
func (a *App) Schema(ctx context.Context) (resourceserver.Schema, error) {
	schemas, err := a.c.Schemas(ctx, a.RSID)
	if err != nil {
		return resourceserver.Schema{}, err
	}
	return schemas[a.RSID], nil
}

// NewRootCmd builds the complete resource-server CLI from spec: a root command
// carrying the shared --config/--base-url/--token/--token-file flags, the
// built-in catalog and sign commands, the declared operation commands, and any
// Extra commands. Output is written to out.
//
// @arg spec The CLI description (name, RS id, operations, extras).
// @arg out The writer command output is written to.
// @return *cobra.Command The root command, ready to Execute.
//
// @testcase TestNewRootCmdWiresBuiltins includes catalog, sign and operation commands.
// @testcase TestCatalogPrintsSchema executes the catalog command built here.
func NewRootCmd(spec Spec, out io.Writer) *cobra.Command {
	a := &App{RSID: spec.RSID, Out: out, defaultBaseURL: spec.DefaultBaseURL}
	root := &cobra.Command{
		Use:           spec.Use,
		Short:         spec.Short,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return a.load(cmd.Flags().Changed("token-file"))
		},
	}
	root.PersistentFlags().StringVar(&a.configPath, "config", defaultConfigPath(spec.RSID), "path to the config file")
	root.PersistentFlags().StringVar(&a.baseURL, "base-url", "", "resource server base URL (overrides config)")
	root.PersistentFlags().StringVar(&a.token, "token", "", "subject token (overrides --token-file)")
	root.PersistentFlags().StringVar(&a.tokenFile, "token-file", "", "file holding the subject token (default ~/.granular/subject_token)")

	root.AddCommand(a.catalogCmd(), a.signCmd())
	root.AddCommand(operationCommands(a, spec.Operations)...)
	if spec.Extra != nil {
		root.AddCommand(spec.Extra(a)...)
	}
	return root
}

// load reads the config file (only the RS base URL) and builds the client into
// the App. The base URL is the --base-url flag, else the config's base_url, else
// the spec default. The token is --token, else read from --token-file, else from
// the default subject token path on disk. tokenFileFlagSet reports whether
// --token-file was given explicitly (which makes a missing file an error).
//
// @arg tokenFileFlagSet Whether --token-file was set explicitly.
// @error error when the config or an explicitly chosen token file cannot be read.
//
// @testcase TestCatalogPrintsSchema builds the client before running a command.
// @testcase TestLoadResolvesBaseURLFromConfig uses the config base URL when no flag is given.
// @testcase TestOperationCommandSendsTypedParams resolves the --token before running.
func (a *App) load(tokenFileFlagSet bool) error {
	cfg, err := loadConfig(a.configPath)
	if err != nil {
		return err
	}

	base := a.baseURL
	if base == "" {
		base = cfg.BaseURL
	}
	if base == "" {
		base = a.defaultBaseURL
	}

	tokenFile := a.tokenFile
	if tokenFile == "" {
		tokenFile = DefaultSubjectTokenPath
	}
	token, err := resolveToken(a.token, tokenFile, tokenFileFlagSet)
	if err != nil {
		return err
	}

	a.c = client.New(client.Config{
		Token:           token,
		ResourceServers: []client.ResourceServer{{ID: a.RSID, BaseURL: base}},
	})
	return nil
}

// operationCommands turns declared operation commands into a cobra command tree,
// creating intermediate group commands for multi-word paths (so "issue create"
// and "issue list" share one "issue" parent).
//
// @arg a The shared App the commands run against.
// @arg ops The declared operation commands.
// @return []*cobra.Command The top-level commands (groups and leaves) to add to the root.
//
// @testcase TestNewRootCmdWiresBuiltins nests multi-word operation paths under a group.
func operationCommands(a *App, ops []OpCommand) []*cobra.Command {
	// groups maps a path prefix to the command created for it, so shared parents
	// are reused. order preserves first-seen order of top-level commands.
	groups := map[string]*cobra.Command{}
	var top []*cobra.Command
	for _, op := range ops {
		parts := strings.Fields(op.Path)
		parent := "" // running prefix key
		var parentCmd *cobra.Command
		// Create/reuse group commands for every path segment but the last.
		for _, seg := range parts[:len(parts)-1] {
			key := strings.TrimSpace(parent + " " + seg)
			g, ok := groups[key]
			if !ok {
				g = &cobra.Command{Use: seg, Short: "Commands for " + seg}
				groups[key] = g
				if parentCmd == nil {
					top = append(top, g)
				} else {
					parentCmd.AddCommand(g)
				}
			}
			parent, parentCmd = key, g
		}
		leaf := operationLeaf(a, parts[len(parts)-1], op)
		if parentCmd == nil {
			top = append(top, leaf)
		} else {
			parentCmd.AddCommand(leaf)
		}
	}
	return top
}
