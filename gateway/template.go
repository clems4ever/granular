package gateway

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/clems4ever/granular/internal/proposal"
)

// findTemplate returns the named template from the schema.
//
// @arg s The schema holding the templates.
// @arg name The template name.
// @return Template The matching template.
// @return bool True when a template with that name exists.
//
// @testcase TestExpandTemplate looks up a known template.
func findTemplate(s Schema, name string) (Template, bool) {
	for _, t := range s.Templates {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

// expandTemplate instantiates a template with the client's bindings, producing the Cedar
// policy and the human-readable presentation. Scope params fill the scope selector (then
// resolved by the schema's ScopeFunc); condition params become Cedar `when` predicates on
// resource attributes; Fixed params are pinned by the author. The result is a single
// permit plus a one-grant presentation whose conditions read in plain language.
//
// @arg s The schema supplying the templates, action vocabulary and scope resolver.
// @arg name The template to instantiate.
// @arg bindings The client-supplied parameter values.
// @return []string The generated Cedar policy texts (one permit).
// @return proposal.Presentation The presentation the AS displays verbatim.
// @error error when the template or an action is unknown, a required param is missing, scope resolution fails, or an operator is invalid.
//
// @testcase TestExpandTemplate expands scope and condition params into a permit.
// @testcase TestExpandTemplateRequiredParam errors when a required param is missing.
// @testcase TestExpandTemplateUnknown errors on an unknown template.
func expandTemplate(s Schema, name string, bindings map[string]string) ([]string, proposal.Presentation, error) {
	var none proposal.Presentation
	t, ok := findTemplate(s, name)
	if !ok {
		return nil, none, fmt.Errorf("unknown template %q", name)
	}
	if s.Scope == nil {
		return nil, none, fmt.Errorf("schema has no scope resolver")
	}

	values := map[string]string{}
	scopeMatch := map[string]string{}
	var preds, conditions []string
	for _, pm := range t.Params {
		v := pm.Fixed
		if v == "" {
			v = bindings[pm.Name]
		}
		if v == "" {
			v = pm.Default
		}
		values[pm.Name] = v
		if v == "" {
			if pm.Required {
				return nil, none, fmt.Errorf("template %q requires parameter %q", name, pm.Name)
			}
			continue
		}
		switch {
		case pm.Field != "":
			scopeMatch[pm.Field] = v
		case pm.Attr != "":
			pred, human, err := conditionLiteral(pm.Attr, pm.Op, v)
			if err != nil {
				return nil, none, err
			}
			preds = append(preds, pred)
			conditions = append(conditions, human)
		}
	}

	et, id, label, err := s.Scope(ResourceSelector{Type: t.Scope, Match: scopeMatch})
	if err != nil {
		return nil, none, err
	}

	lits := make([]string, 0, len(t.Actions))
	for _, a := range t.Actions {
		if !s.HasAction(a) {
			return nil, none, fmt.Errorf("template %q names unknown action %q", name, a)
		}
		lits = append(lits, entityLiteral(s.ActionType, a))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "permit (\n  principal == %s,\n  action in [%s],\n  resource in %s\n)",
		entityLiteral(s.AgentType, s.AgentID), strings.Join(lits, ", "), entityLiteral(et, id))
	if len(preds) > 0 {
		fmt.Fprintf(&b, " when { %s }", strings.Join(preds, " && "))
	}
	b.WriteString(";")

	pres := proposal.Presentation{
		Title:   t.Title,
		Summary: renderSummary(t.Summary, values),
		Grants:  []proposal.GrantDetail{{Actions: t.Actions, Resource: label, Conditions: conditions}},
	}
	return []string{b.String()}, pres, nil
}

// conditionLiteral renders one attribute condition as both a Cedar predicate and a
// plain-language phrase.
//
// @arg attr The resource attribute name, e.g. "state".
// @arg op The operator: "eq", "contains" or "like".
// @arg value The value to compare against.
// @return string The Cedar predicate, e.g. resource.state == "open".
// @return string The plain-language phrase, e.g. "state is open".
// @error error when the operator is not recognised.
//
// @testcase TestExpandTemplate renders eq, contains and like conditions.
func conditionLiteral(attr, op, value string) (string, string, error) {
	q := strconv.Quote(value)
	switch op {
	case "eq", "":
		return "resource." + attr + " == " + q, attr + " is " + value, nil
	case "contains":
		return "resource." + attr + ".contains(" + q + ")", attr + " contains " + value, nil
	case "like":
		return "resource." + attr + " like " + q, attr + " matches " + value, nil
	default:
		return "", "", fmt.Errorf("unsupported condition operator %q", op)
	}
}

// renderSummary substitutes each {name} placeholder in a template summary with its bound
// value (empty for unset optional params).
//
// @arg summary The summary string with {param} placeholders.
// @arg values The resolved parameter values.
// @return string The rendered summary.
//
// @testcase TestExpandTemplate substitutes bound values into the summary.
func renderSummary(summary string, values map[string]string) string {
	for name, v := range values {
		summary = strings.ReplaceAll(summary, "{"+name+"}", v)
	}
	return summary
}
