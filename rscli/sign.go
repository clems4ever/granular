package rscli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/clems4ever/granular/resourceserver"
	"github.com/spf13/cobra"
)

// signCmd builds the `sign` command: it assembles a grant request (from a
// template, or freeform from --actions/--resource/--match), has the target
// resource server sign it, and writes the signed request as JSON to --out (or
// stdout). The output is handed to the granular CLI's `propose` command for
// human approval.
//
// @return *cobra.Command The sign command.
//
// @testcase TestSignWritesSignedRequest signs a freeform request and writes it.
// @testcase TestSignToFile writes the signed request to a file.
func (a *App) signCmd() *cobra.Command {
	var reason, resType, match, out, template string
	var actions, binds []string
	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Build a grant request (freeform or from a template) and have the resource server sign it",
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := buildSignRequest(template, binds, reason, actions, resType, match)
			if err != nil {
				return err
			}
			return runSign(context.Background(), a, req, out)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "file to write the signed request to (default stdout)")
	// Template form.
	cmd.Flags().StringVar(&template, "template", "", "instantiate a resource server template instead of a freeform capability")
	cmd.Flags().StringArrayVar(&binds, "bind", nil, "template parameter, name=value (repeatable)")
	// Freeform form.
	cmd.Flags().StringVar(&reason, "reason", "", "human-readable reason shown on the consent screen")
	cmd.Flags().StringSliceVar(&actions, "actions", nil, "action or group names to request")
	cmd.Flags().StringVar(&resType, "resource", "", "resource type the capability is scoped to")
	cmd.Flags().StringVar(&match, "match", "", "resource match fields, key=value,key=value")
	return cmd
}

// buildSignRequest assembles a grant request from the sign flags: a template
// instantiation when --template is set, otherwise a one-capability freeform
// request.
//
// @arg template The template name, or "" for freeform.
// @arg binds The template bindings as "name=value" strings.
// @arg reason The human-readable reason (freeform).
// @arg actions The requested action or group names (freeform).
// @arg resType The resource type the capability is scoped to (freeform).
// @arg match The resource match fields as "key=value,key=value" (freeform).
// @return resourceserver.GrantRequest The assembled grant request.
// @error error when a template binding is malformed.
//
// @testcase TestBuildSignRequestTemplate builds a template request.
// @testcase TestBuildSignRequestFreeform builds a freeform capability request.
func buildSignRequest(template string, binds []string, reason string, actions []string, resType, match string) (resourceserver.GrantRequest, error) {
	if template != "" {
		bindings, err := parseBinds(binds)
		if err != nil {
			return resourceserver.GrantRequest{}, err
		}
		return resourceserver.GrantRequest{Template: template, Bindings: bindings}, nil
	}
	return resourceserver.GrantRequest{
		Reason: reason,
		Capabilities: []resourceserver.Capability{{
			Actions:  actions,
			Resource: resourceserver.ResourceSelector{Type: resType, Match: parseMatch(match)},
		}},
	}, nil
}

// runSign signs the grant request against the target resource server and writes
// the signed result as indented JSON to outPath, or to the App's writer when
// outPath is empty.
//
// @arg ctx Context for cancellation.
// @arg a The shared App (client and output writer).
// @arg req The grant request to sign.
// @arg outPath The file to write to, or "" for the App writer.
// @error error when signing, marshalling, or writing fails.
//
// @testcase TestSignWritesSignedRequest writes the signed request to the writer.
// @testcase TestSignToFile writes the signed request to a file.
func runSign(ctx context.Context, a *App, req resourceserver.GrantRequest, outPath string) error {
	signed, err := a.Sign(ctx, req)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		return err
	}
	if outPath == "" {
		_, err := a.Out.Write(append(data, '\n'))
		return err
	}
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(a.Out, "wrote signed grant request to %s\n", outPath)
	return nil
}

// parseBinds parses repeated "name=value" template bindings into a map.
//
// @arg binds The "name=value" strings.
// @return map[string]string The parsed bindings.
// @error error when an entry is missing the "=" separator.
//
// @testcase TestBuildSignRequestTemplate parses bindings; a malformed one errors.
func parseBinds(binds []string) (map[string]string, error) {
	out := make(map[string]string, len(binds))
	for _, b := range binds {
		k, v, ok := strings.Cut(b, "=")
		if !ok {
			return nil, fmt.Errorf("invalid binding %q (want name=value)", b)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

// parseMatch parses a "key=value,key=value" string into a resource match map; an
// empty string yields an empty map.
//
// @arg s The comma-separated key=value pairs (may be empty).
// @return map[string]string The parsed match fields.
//
// @testcase TestBuildSignRequestFreeform parses match fields into a selector.
func parseMatch(s string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		if k, v, ok := strings.Cut(pair, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}
