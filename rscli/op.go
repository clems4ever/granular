package rscli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/clems4ever/granular/client"
	"github.com/clems4ever/granular/resourceserver"
	"github.com/spf13/cobra"
)

// operationLeaf builds the cobra command that runs one operation: it registers a
// typed flag per Flag, and on run collects the set flags into native-typed params
// and executes the operation, printing the JSON result (or an actionable message
// on an authorization denial).
//
// @arg a The shared App the command runs against.
// @arg use The command's own name (the last path segment).
// @arg op The operation command declaration.
// @return *cobra.Command The runnable operation command.
//
// @testcase TestOperationCommandSendsTypedParams maps typed flags to native params.
// @testcase TestOperationCommandDenialMessage prints guidance on a denial.
func operationLeaf(a *App, use string, op OpCommand) *cobra.Command {
	// Per-flag value holders, keyed by flag name.
	strs := map[string]*string{}
	ints := map[string]*int{}
	bools := map[string]*bool{}
	slices := map[string]*[]string{}

	cmd := &cobra.Command{
		Use:   use,
		Short: op.Short,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{}
			for _, f := range op.Flags {
				// Skip optional flags the user did not set, so resource-server
				// defaults are preserved; always send required flags.
				if !f.Required && !cmd.Flags().Changed(f.Name) {
					continue
				}
				switch f.Type {
				case IntFlag:
					params[f.param()] = *ints[f.Name]
				case BoolFlag:
					params[f.param()] = *bools[f.Name]
				case StringSliceFlag:
					params[f.param()] = *slices[f.Name]
				default:
					params[f.param()] = *strs[f.Name]
				}
			}
			return runOp(context.Background(), a, op.Type, params)
		},
	}
	for _, f := range op.Flags {
		switch f.Type {
		case IntFlag:
			ints[f.Name] = cmd.Flags().Int(f.Name, 0, f.Usage)
		case BoolFlag:
			bools[f.Name] = cmd.Flags().Bool(f.Name, false, f.Usage)
		case StringSliceFlag:
			slices[f.Name] = cmd.Flags().StringSlice(f.Name, nil, f.Usage)
		default:
			strs[f.Name] = cmd.Flags().String(f.Name, "", f.Usage)
		}
		if f.Required {
			_ = cmd.MarkFlagRequired(f.Name)
		}
	}
	return cmd
}

// runOp executes the operation and renders the outcome: the JSON result on
// success, or an actionable message (then the error) when the policy denies it.
//
// @arg ctx Context for cancellation.
// @arg a The shared App (client and output writer).
// @arg opType The operation type id to run.
// @arg params The native-typed operation parameters.
// @error error from the run (including client.ErrNotAuthorized on denial).
//
// @testcase TestOperationCommandSendsTypedParams runs an operation and checks the result.
// @testcase TestOperationCommandDenialMessage surfaces the denial guidance and error.
func runOp(ctx context.Context, a *App, opType string, params map[string]any) error {
	res, err := a.Run(ctx, resourceserver.OperationRequest{Type: opType, Params: params})
	if err == client.ErrNotAuthorized {
		fmt.Fprintln(a.Out, "Not authorized. Build a grant request with the `sign` command, submit it with `granular propose`, have it approved, then retry.")
		return err
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}
