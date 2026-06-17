package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/clems4ever/granular/client"
	"github.com/clems4ever/granular/gateway"
	"github.com/clems4ever/granular/internal/proposal"
)

// parseParams parses repeated "key=value" strings into an operation parameter map.
//
// @arg pairs The "key=value" strings.
// @return map[string]any The parsed parameters.
// @error error when an entry is missing the "=" separator.
//
// @testcase TestParseParams parses pairs and rejects a malformed entry.
func parseParams(pairs []string) (map[string]any, error) {
	out := make(map[string]any, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return nil, fmt.Errorf("invalid parameter %q (want key=value)", p)
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

// parseMatch parses a "key=value,key=value" string into a resource match map.
//
// @arg s The comma-separated key=value pairs (may be empty).
// @return map[string]string The parsed match fields.
//
// @testcase TestParseMatch parses multiple fields and the empty string.
func parseMatch(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if k, v, ok := strings.Cut(part, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

// buildGrantRequest assembles a one-capability grant request from the sign flags.
//
// @arg reason The human-readable reason shown on the consent screen.
// @arg actions The action or group names to request.
// @arg resType The resource type the capability is scoped to.
// @arg match The resource match fields.
// @return gateway.GrantRequest The assembled grant request.
//
// @testcase TestBuildGrantRequest builds a request from flags.
func buildGrantRequest(reason string, actions []string, resType string, match map[string]string) gateway.GrantRequest {
	return gateway.GrantRequest{
		Reason: reason,
		Capabilities: []gateway.Capability{{
			Actions:  actions,
			Resource: gateway.ResourceSelector{Type: resType, Match: match},
		}},
	}
}

// runCatalog fetches and prints the schemas of the requested gateways (or all of them).
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg ids An optional subset of gateway ids.
// @arg w The writer for user-facing output.
// @error error when a gateway request fails.
//
// @testcase TestRunCatalog prints a gateway's actions.
func runCatalog(ctx context.Context, c *client.Client, ids []string, w io.Writer) error {
	schemas, err := c.Schemas(ctx, ids...)
	if err != nil {
		return err
	}
	gws := make([]string, 0, len(schemas))
	for id := range schemas {
		gws = append(gws, id)
	}
	sort.Strings(gws)
	for _, id := range gws {
		fmt.Fprintf(w, "%s:\n", id)
		for _, a := range schemas[id].Actions {
			fmt.Fprintf(w, "  %-22s %s\n", a.Name, a.Title)
		}
	}
	return nil
}

// runOp runs an operation on a gateway and prints its result, turning a denial into a
// clear, actionable message.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg gatewayID The gateway to run on.
// @arg opType The operation type.
// @arg params The operation parameters.
// @arg w The writer for user-facing output.
// @error error when the operation is unauthorized or the call fails.
//
// @testcase TestRunOp prints the result of an authorized operation.
func runOp(ctx context.Context, c *client.Client, gatewayID, opType string, params map[string]any, w io.Writer) error {
	res, err := c.Run(ctx, gatewayID, gateway.OperationRequest{Type: opType, Params: params})
	if err == client.ErrNotAuthorized {
		fmt.Fprintf(w, "Not authorized. Sign a grant request (granular sign ...), submit it (granular propose ...), have it approved, then retry.\n")
		return err
	}
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// runSign signs a grant request against one gateway and writes the gateway-signed result
// (as JSON) to a file, or to the output writer when no path is given, so it can be stored
// and later submitted with the propose command.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg gatewayID The gateway to sign against.
// @arg req The grant request to sign.
// @arg outPath The file to write the signed request to, or "" for the output writer.
// @arg w The writer for user-facing output.
// @error error when signing or writing fails.
//
// @testcase TestRunSign signs a request and writes it to a file.
func runSign(ctx context.Context, c *client.Client, gatewayID string, req gateway.GrantRequest, outPath string, w io.Writer) error {
	signed, err := c.Sign(ctx, gatewayID, req)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		return err
	}
	if outPath == "" {
		_, err := w.Write(append(data, '\n'))
		return err
	}
	if err := os.WriteFile(outPath, data, 0o600); err != nil {
		return err
	}
	fmt.Fprintf(w, "wrote signed grant request to %s\n", outPath)
	return nil
}

// runPropose reads one or more stored signed grant requests, packs them into a single
// proposal, submits it to the AS, and prints the approval URL to hand to the user.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg approver The email of the human who must approve.
// @arg files The paths of the stored signed grant requests to bundle.
// @arg w The writer for user-facing output.
// @error error when a file cannot be read or the submission fails.
//
// @testcase TestRunPropose bundles signed requests and prints the approval URL.
func runPropose(ctx context.Context, c *client.Client, approver string, files []string, w io.Writer) error {
	items, err := readSignedRequests(files)
	if err != nil {
		return err
	}
	p, err := c.Submit(ctx, approver, items)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Proposal %s created. Open this URL to approve or deny:\n\n  %s\n", p.ID, p.URL)
	return nil
}

// readSignedRequests reads and decodes each named file into a signed grant request.
//
// @arg files The paths of the JSON-encoded signed grant requests.
// @return []proposal.SignedGrantRequest The decoded signed grant requests.
// @error error when a file cannot be read or does not decode.
//
// @testcase TestRunPropose reads signed requests from files.
func readSignedRequests(files []string) ([]proposal.SignedGrantRequest, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no signed grant request files given")
	}
	items := make([]proposal.SignedGrantRequest, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var sgr proposal.SignedGrantRequest
		if err := json.Unmarshal(data, &sgr); err != nil {
			return nil, fmt.Errorf("decode %s: %w", f, err)
		}
		items = append(items, sgr)
	}
	return items, nil
}
