package rscli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/clems4ever/granular/resourceserver"
	"github.com/spf13/cobra"
)

// catalogCmd builds the `catalog` command: it prints the target resource
// server's permission schema — its resources and match fields, action groups,
// actions, operations, and templates — in human-readable form, or as raw JSON
// with --json.
//
// @return *cobra.Command The catalog command.
//
// @testcase TestCatalogPrintsSchema prints the human-readable schema.
// @testcase TestCatalogJSON prints the raw schema as JSON.
func (a *App) catalogCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Print the resource server's permission schema (resources, actions, operations, templates)",
		RunE: func(cmd *cobra.Command, args []string) error {
			schema, err := a.Schema(context.Background())
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(a.Out)
				enc.SetIndent("", "  ")
				return enc.Encode(schema)
			}
			printSchema(a.Out, a.RSID, schema)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the raw schema as JSON for programmatic use")
	return cmd
}

// printSchema renders a resource server's schema as human-readable text: the
// resource hierarchy with match fields, action groups expanded to their actions,
// actions with their target resource, operations with parameters (required ones
// starred), and templates.
//
// @arg w The writer to render to.
// @arg id The resource server id (heading).
// @arg s The schema to render.
//
// @testcase TestCatalogPrintsSchema checks resources, operations and templates render.
func printSchema(w io.Writer, id string, s resourceserver.Schema) {
	fmt.Fprintf(w, "%s\n\n", id)

	fmt.Fprintf(w, "  Resources — a grant's resource.type and the match fields a selector may set:\n")
	for _, r := range s.Resources {
		fmt.Fprintf(w, "    %-14s %s\n", r.Name, r.Title)
		for _, m := range r.Match {
			fmt.Fprintf(w, "        %-9s %-20s %s\n", m.Name, "("+m.Type+")", m.Description)
		}
	}

	if len(s.Groups) > 0 {
		fmt.Fprintf(w, "\n  Groups — usable as a grant action; each rolls up these actions:\n")
		for _, g := range s.Groups {
			fmt.Fprintf(w, "    %-14s %s\n", g.Name, g.Title)
		}
	}

	fmt.Fprintf(w, "\n  Actions — usable as a grant action; each acts on one resource type:\n")
	for _, ac := range s.Actions {
		fmt.Fprintf(w, "    %-14s on %s\n", ac.Name, ac.Resource)
	}

	if len(s.Operations) > 0 {
		fmt.Fprintf(w, "\n  Operations — run as sub-commands (* = required flag):\n")
		for _, op := range s.Operations {
			mut := ""
			if op.Mutating {
				mut = " [mutating]"
			}
			fmt.Fprintf(w, "    %s%s — needs action %s on %s\n", op.Type, mut, op.Action, op.Resource)
			for _, p := range op.Params {
				star := " "
				if p.Required {
					star = "*"
				}
				fmt.Fprintf(w, "        %s%-9s %-26s %s\n", star, p.Name, "("+p.Type+")", p.Description)
			}
		}
	}

	if len(s.Templates) > 0 {
		fmt.Fprintf(w, "\n  Templates — sign with: sign --template <name> --bind p=v:\n")
		for _, t := range s.Templates {
			fmt.Fprintf(w, "    %-24s %s — grants %s on a %s\n", t.Name, t.Title, strings.Join(t.Actions, ", "), t.Scope)
		}
	}
	fmt.Fprintln(w)
}
