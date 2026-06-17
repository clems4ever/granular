package clientcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/clems4ever/granular/client"
	"github.com/clems4ever/granular/internal/proposal"
	"github.com/clems4ever/granular/resourceserver"
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

// buildSignRequest assembles the grant request the sign command sends: a template
// instantiation when a template name is given, otherwise a freeform one-capability
// request from the resource flags.
//
// @arg template The template name, or "" for the freeform form.
// @arg binds The template "name=value" bindings (template form only).
// @arg reason The freeform reason.
// @arg actions The freeform action or group names.
// @arg resType The freeform resource type.
// @arg match The freeform "key=value,key=value" resource match.
// @return resourceserver.GrantRequest The assembled request.
// @error error when a template binding is malformed.
//
// @testcase TestBuildSignRequest builds both the template and freeform forms.
func buildSignRequest(template string, binds []string, reason string, actions []string, resType, match string) (resourceserver.GrantRequest, error) {
	if template != "" {
		bindings, err := parseBinds(binds)
		if err != nil {
			return resourceserver.GrantRequest{}, err
		}
		return resourceserver.GrantRequest{Template: template, Bindings: bindings}, nil
	}
	return buildGrantRequest(reason, actions, resType, parseMatch(match)), nil
}

// parseBinds parses repeated "name=value" template bindings into a map.
//
// @arg binds The "name=value" strings.
// @return map[string]string The parsed bindings.
// @error error when an entry is missing the "=" separator.
//
// @testcase TestBuildSignRequest rejects a malformed binding.
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

// buildGrantRequest assembles a one-capability grant request from the sign flags.
//
// @arg reason The human-readable reason shown on the consent screen.
// @arg actions The action or group names to request.
// @arg resType The resource type the capability is scoped to.
// @arg match The resource match fields.
// @return resourceserver.GrantRequest The assembled grant request.
//
// @testcase TestBuildGrantRequest builds a request from flags.
func buildGrantRequest(reason string, actions []string, resType string, match map[string]string) resourceserver.GrantRequest {
	return resourceserver.GrantRequest{
		Reason: reason,
		Capabilities: []resourceserver.Capability{{
			Actions:  actions,
			Resource: resourceserver.ResourceSelector{Type: resType, Match: match},
		}},
	}
}

// runCatalog fetches the schemas of the requested resource servers (or all of them) and prints
// everything an agent needs to build a grant request: the resource types with their
// match fields and hierarchy, the action groups, every action with the resource type it
// targets, and a ready-to-use example. With asJSON it prints the raw schemas instead, for
// programmatic consumption.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg ids An optional subset of resource server ids.
// @arg asJSON Whether to print the raw schema JSON instead of the human-readable form.
// @arg w The writer for user-facing output.
// @error error when a resource server request fails or the JSON cannot be encoded.
//
// @testcase TestRunCatalog prints resources, actions and an example.
// @testcase TestRunCatalogJSON prints the raw schema as JSON.
func runCatalog(ctx context.Context, c *client.Client, ids []string, asJSON bool, w io.Writer) error {
	schemas, err := c.Schemas(ctx, ids...)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(schemas)
	}
	rsIDs := make([]string, 0, len(schemas))
	for id := range schemas {
		rsIDs = append(rsIDs, id)
	}
	sort.Strings(rsIDs)
	for _, id := range rsIDs {
		printSchema(w, id, schemas[id])
	}
	return nil
}

// printSchema renders one resource server's schema in a form an agent can act on: the resource
// hierarchy with each type's match fields (for a grant's resource selector), the groups
// expanded to the actions they grant, the action vocabulary, and the executable
// operations with their full parameter signatures (for running work).
//
// @arg w The writer for user-facing output.
// @arg id The resource server id.
// @arg s The resource server's permission schema.
//
// @testcase TestRunCatalog renders resources, groups, operations and an example.
func printSchema(w io.Writer, id string, s resourceserver.Schema) {
	fmt.Fprintf(w, "%s\n\n", id)

	fmt.Fprintf(w, "  Resources — a grant's resource.type and the match fields a selector may set:\n")
	for _, r := range s.Resources {
		indent := strings.Repeat("  ", resourceDepth(s, r.Name))
		fmt.Fprintf(w, "    %s%-14s %s\n", indent, r.Name, r.Title)
		for _, m := range r.Match {
			fmt.Fprintf(w, "    %s    %-9s %-20s %s\n", indent, m.Name, "("+m.Type+")", m.Description)
		}
	}

	if len(s.Groups) > 0 {
		fmt.Fprintf(w, "\n  Groups — usable as a grant action; each grants these concrete actions:\n")
		expand := groupActions(s)
		for _, g := range s.Groups {
			fmt.Fprintf(w, "    %-14s → %s\n", g.Name, strings.Join(expand[g.Name], ", "))
		}
	}

	fmt.Fprintf(w, "\n  Actions — usable as a grant action; each acts on one resource type:\n")
	for _, a := range s.Actions {
		fmt.Fprintf(w, "    %-14s on %s\n", a.Name, a.Resource)
	}

	if len(s.Operations) > 0 {
		fmt.Fprintf(w, "\n  Operations — run with: granular op %s <type> -p key=value ... (* = required):\n", id)
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
		fmt.Fprintf(w, "\n  Templates — granular template <name> for detail; sign with --template <name> --bind p=v:\n")
		for _, t := range s.Templates {
			fmt.Fprintf(w, "    %-24s %s — grants %s on a %s\n", t.Name, t.Title, strings.Join(t.Actions, ", "), t.Scope)
		}
	}

	if len(s.Example.Capabilities) > 0 {
		fmt.Fprintf(w, "\n  Example — sign this with: granular sign --resource-server %s ...\n", id)
		for _, cap := range s.Example.Capabilities {
			fmt.Fprintf(w, "    --actions %s --resource %s --match %s\n",
				strings.Join(cap.Actions, ","), cap.Resource.Type, formatMatch(cap.Resource.Match))
		}
	}
	fmt.Fprintln(w)
}

// resourceDepth returns how many parents a resource type has in the hierarchy, for
// indented tree rendering.
//
// @arg s The schema holding the resource types.
// @arg name The resource type name.
// @return int The number of ancestors above the resource.
//
// @testcase TestRunCatalog indents nested resources.
func resourceDepth(s resourceserver.Schema, name string) int {
	parent := map[string]string{}
	for _, r := range s.Resources {
		parent[r.Name] = r.Parent
	}
	depth := 0
	for p := parent[name]; p != ""; p = parent[p] {
		depth++
		if depth > len(s.Resources) {
			break // guard against a cycle
		}
	}
	return depth
}

// groupActions maps each group to the concrete action names it grants transitively,
// following the group lattice upward from every action's groups.
//
// @arg s The schema holding the groups and actions.
// @return map[string][]string Each group name mapped to its granted action names (sorted).
//
// @testcase TestRunCatalog expands a group to its actions.
func groupActions(s resourceserver.Schema) map[string][]string {
	parents := map[string][]string{}
	for _, g := range s.Groups {
		parents[g.Name] = g.Parents
	}
	// ancestorsOf returns a group and all groups reachable upward from it.
	ancestorsOf := func(start string) map[string]bool {
		seen := map[string]bool{}
		var visit func(string)
		visit = func(n string) {
			if seen[n] {
				return
			}
			seen[n] = true
			for _, p := range parents[n] {
				visit(p)
			}
		}
		visit(start)
		return seen
	}
	out := map[string][]string{}
	for _, a := range s.Actions {
		for _, g := range a.Groups {
			for anc := range ancestorsOf(g) {
				out[anc] = append(out[anc], a.Name)
			}
		}
	}
	for g := range out {
		sort.Strings(out[g])
	}
	return out
}

// formatMatch renders a resource match map as the comma-separated key=value form the sign
// command's --match flag expects, with keys in a stable order.
//
// @arg match The resource match fields.
// @return string The "key=value,key=value" rendering.
//
// @testcase TestRunCatalog renders the example's match fields.
func formatMatch(match map[string]string) string {
	keys := make([]string, 0, len(match))
	for k := range match {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + match[k]
	}
	return strings.Join(parts, ",")
}

// runTemplate explores grant templates: with no name it lists every template (across the
// requested resource servers), and with a name it prints what that template actually grants — the
// actions (groups expanded), the scope, the attribute conditions (fixed and parameterized),
// the summary, and the parameters with how each is used.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg ids An optional subset of resource server ids.
// @arg name The template to detail, or "" to list all templates.
// @arg w The writer for user-facing output.
// @error error when a resource server request fails or the named template is not found.
//
// @testcase TestRunTemplate lists templates and details one by name.
func runTemplate(ctx context.Context, c *client.Client, ids []string, name string, w io.Writer) error {
	schemas, err := c.Schemas(ctx, ids...)
	if err != nil {
		return err
	}
	rsIDs := make([]string, 0, len(schemas))
	for id := range schemas {
		rsIDs = append(rsIDs, id)
	}
	sort.Strings(rsIDs)

	found := false
	for _, id := range rsIDs {
		s := schemas[id]
		for _, t := range s.Templates {
			if name == "" {
				fmt.Fprintf(w, "%-24s %s — grants %s on a %s  [%s]\n", t.Name, t.Title, strings.Join(t.Actions, ", "), t.Scope, id)
				found = true
				continue
			}
			if t.Name == name {
				printTemplateDetail(w, id, s, t)
				found = true
			}
		}
	}
	if name != "" && !found {
		return fmt.Errorf("no template named %q", name)
	}
	if !found {
		fmt.Fprintln(w, "no templates available")
	}
	return nil
}

// printTemplateDetail renders the full detail of one template: what it grants, its scope,
// its conditions, its summary, and its parameters.
//
// @arg w The writer for user-facing output.
// @arg resourceServerID The resource server offering the template.
// @arg s The schema (for expanding action groups).
// @arg t The template to detail.
//
// @testcase TestRunTemplate renders a template's grants, conditions and parameters.
func printTemplateDetail(w io.Writer, resourceServerID string, s resourceserver.Schema, t resourceserver.Template) {
	fmt.Fprintf(w, "%s — %s   [resource server %s]\n", t.Name, t.Title, resourceServerID)
	if t.Description != "" {
		fmt.Fprintf(w, "\n  %s\n", t.Description)
	}

	fmt.Fprintf(w, "\n  Grants:  %s\n", strings.Join(t.Actions, ", "))
	if concrete := templateActions(s, t); len(concrete) > 0 {
		fmt.Fprintf(w, "           (i.e. %s)\n", strings.Join(concrete, ", "))
	}
	fmt.Fprintf(w, "  Scope:   a %s (and resources under it)\n", t.Scope)

	var conditions []string
	for _, p := range t.Params {
		if p.Attr == "" {
			continue
		}
		if p.Fixed != "" {
			conditions = append(conditions, conditionPhrase(p.Attr, p.Op, p.Fixed)+" (fixed)")
		} else {
			when := "when set"
			if p.Required {
				when = "required"
			}
			conditions = append(conditions, conditionPhrase(p.Attr, p.Op, "<"+p.Name+">")+" ("+when+")")
		}
	}
	if len(conditions) > 0 {
		fmt.Fprintf(w, "  Limited to resources where:\n")
		for _, cnd := range conditions {
			fmt.Fprintf(w, "    - %s\n", cnd)
		}
	}
	if t.Summary != "" {
		fmt.Fprintf(w, "  Consent: %q\n", t.Summary)
	}

	fmt.Fprintf(w, "\n  Parameters (bind with --bind name=value):\n")
	for _, p := range t.Params {
		if p.Fixed != "" {
			fmt.Fprintf(w, "    %-10s pinned to %q by the resource server\n", p.Name, p.Fixed)
			continue
		}
		role := "scope (" + p.Field + ")"
		if p.Attr != "" {
			role = "condition (" + p.Attr + ")"
		}
		flag := " "
		if p.Required {
			flag = "*"
		}
		fmt.Fprintf(w, "    %s%-10s %-18s %s\n", flag, p.Name, role, p.Description)
	}

	fmt.Fprintf(w, "\n  Sign: granular sign --resource-server %s --template %s%s\n\n", resourceServerID, t.Name, bindHints(t))
}

// templateActions expands a template's granted actions/groups to the concrete action
// names they ultimately grant.
//
// @arg s The schema supplying the group lattice.
// @arg t The template whose actions are expanded.
// @return []string The concrete action names granted (sorted, deduplicated).
//
// @testcase TestRunTemplate expands a template's granted group to its actions.
func templateActions(s resourceserver.Schema, t resourceserver.Template) []string {
	expand := groupActions(s)
	seen := map[string]bool{}
	var out []string
	for _, a := range t.Actions {
		members := expand[a]
		if members == nil {
			members = []string{a} // a concrete action, not a group
		}
		for _, m := range members {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Strings(out)
	return out
}

// conditionPhrase renders an attribute condition in plain language, mirroring the
// resource server's expander.
//
// @arg attr The resource attribute.
// @arg op The operator: eq, contains or like.
// @arg value The value or a <placeholder>.
// @return string The plain-language phrase.
//
// @testcase TestRunTemplate phrases fixed and parameterized conditions.
func conditionPhrase(attr, op, value string) string {
	switch op {
	case "contains":
		return attr + " contains " + value
	case "like":
		return attr + " matches " + value
	default:
		return attr + " is " + value
	}
}

// bindHints builds the trailing "--bind name=…" hints for a template's bindable params.
//
// @arg t The template.
// @return string The concatenated bind hints (empty when all params are fixed).
//
// @testcase TestRunTemplate includes bind hints in the sign example.
func bindHints(t resourceserver.Template) string {
	var b strings.Builder
	for _, p := range t.Params {
		if p.Fixed == "" {
			fmt.Fprintf(&b, " --bind %s=…", p.Name)
		}
	}
	return b.String()
}

// runOp runs an operation on a resource server and prints its result, turning a denial into a
// clear, actionable message.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg resourceServerID The resource server to run on.
// @arg opType The operation type.
// @arg params The operation parameters.
// @arg w The writer for user-facing output.
// @error error when the operation is unauthorized or the call fails.
//
// @testcase TestRunOp prints the result of an authorized operation.
func runOp(ctx context.Context, c *client.Client, resourceServerID, opType string, params map[string]any, w io.Writer) error {
	res, err := c.Run(ctx, resourceServerID, resourceserver.OperationRequest{Type: opType, Params: params})
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

// runSign signs a grant request against one resource server and writes the resource server-signed result
// (as JSON) to a file, or to the output writer when no path is given, so it can be stored
// and later submitted with the propose command.
//
// @arg ctx Context for cancellation.
// @arg c The client SDK.
// @arg resourceServerID The resource server to sign against.
// @arg req The grant request to sign.
// @arg outPath The file to write the signed request to, or "" for the output writer.
// @arg w The writer for user-facing output.
// @error error when signing or writing fails.
//
// @testcase TestRunSign signs a request and writes it to a file.
func runSign(ctx context.Context, c *client.Client, resourceServerID string, req resourceserver.GrantRequest, outPath string, w io.Writer) error {
	signed, err := c.Sign(ctx, resourceServerID, req)
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
	if p.ExpiresAt != "" {
		fmt.Fprintf(w, "\nThis request expires at %s — approve before then or submit a new one.\n", p.ExpiresAt)
	}
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
