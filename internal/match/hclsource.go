package match

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// terraformSource extracts the source address governing the required_providers
// version attribute at lines[target], parsing the whole file as HCL and
// locating the entry whose version expression sits on that line. The line
// alone names nothing - the source lives on a sibling line of the entry - so
// this is the one context-aware inference. It returns "" when the file does
// not parse or the line belongs to no required_providers entry (a module
// block's version, for instance, pins a module, not a provider).
func terraformSource(lines []string, target int) string {
	file, diags := hclsyntax.ParseConfig(
		[]byte(strings.Join(lines, "\n")),
		"",
		hcl.InitialPos,
	)
	if diags.HasErrors() {
		return ""
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return ""
	}

	for _, tf := range body.Blocks {
		if tf.Type != "terraform" {
			continue
		}
		for _, rp := range tf.Body.Blocks {
			if rp.Type != "required_providers" {
				continue
			}
			for _, attr := range rp.Body.Attributes {
				if source, ok := entrySource(attr, target+1); ok {
					return source
				}
			}
		}
	}
	return ""
}

// entrySource reports the entry's source attribute when its version attribute
// lives on the 1-based line. A shorthand string entry (aws = "~> 6.39") never
// reaches here: its line carries the provider name, not a version key, so the
// route cannot match it.
func entrySource(attr *hclsyntax.Attribute, line int) (string, bool) {
	object, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return "", false
	}

	source := ""
	onLine := false
	for _, item := range object.Items {
		key := hcl.ExprAsKeyword(item.KeyExpr)
		switch key {
		case "source":
			source = stringValue(item.ValueExpr)
		case "version":
			onLine = spans(item.ValueExpr.Range(), line)
		}
	}
	if !onLine {
		return "", false
	}
	return source, source != ""
}

// spans reports whether a source range covers the 1-based line.
func spans(r hcl.Range, line int) bool {
	return r.Start.Line <= line && line <= r.End.Line
}

// stringValue evaluates an expression to its literal string, "" when it is not
// a constant string (a variable reference cannot name a source).
func stringValue(expr hclsyntax.Expression) string {
	v, diags := expr.Value(nil)
	if diags.HasErrors() || v.IsNull() || v.Type().FriendlyName() != "string" {
		return ""
	}
	return v.AsString()
}
