package match

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// registrySource extracts the registry source address governing the version
// attribute at lines[target], parsing the whole file as HCL. The line alone
// names nothing - the source lives on a sibling line of the enclosing block -
// which is why an inference reads a whole [subject] rather than its target line.
//
// Both things a Terraform registry serves are reached this way: a provider named
// by a required_providers entry, and a module named by a module block. They are
// looked up behind one parse, since a version attribute is at most one of the
// two and an annotate scan asks per candidate line. It returns "" when the file
// does not parse or the line belongs to neither.
func registrySource(lines []string, target int) string {
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
	if source := providerSource(body, target+1); source != "" {
		return source
	}
	return moduleSource(body, target+1)
}

// providerSource returns the source of the required_providers entry whose
// version attribute sits on the 1-based line, or "" when the line belongs to no
// such entry.
func providerSource(body *hclsyntax.Body, line int) string {
	for _, tf := range body.Blocks {
		if tf.Type != "terraform" {
			continue
		}
		for _, rp := range tf.Body.Blocks {
			if rp.Type != "required_providers" {
				continue
			}
			for _, attr := range rp.Body.Attributes {
				if source, ok := entrySource(attr, line); ok {
					return source
				}
			}
		}
	}
	return ""
}

// moduleSource returns the registry address of the module block whose version
// attribute sits on the 1-based line.
//
// Only a registry address is returned. A module may equally be sourced from a
// local path, a git or archive URL, or a shorthand naming a forge, none of which
// the registry serves and all of which carry their own ref if they are versioned
// at all - so such a block infers nothing rather than resolving against a
// registry that never published it.
func moduleSource(body *hclsyntax.Body, line int) string {
	for _, block := range body.Blocks {
		if block.Type != "module" {
			continue
		}
		version, ok := block.Body.Attributes["version"]
		if !ok || !spans(version.Expr.Range(), line) {
			continue
		}
		source, ok := block.Body.Attributes["source"]
		if !ok {
			return ""
		}
		address := stringValue(source.Expr)
		if !registryAddress(address) {
			return ""
		}
		return address
	}
	return ""
}

// registryAddress reports whether a module source is a plain registry address,
// namespace/name/target, rather than something the registry does not serve.
// Terraform treats a source with no recognized scheme or path prefix as a
// registry address, so the test is by elimination.
//
// A first segment carrying a dot or colon is a hostname, and a registry
// namespace can carry neither - the same signal Terraform's own address parser
// uses, and the one the provider disambiguates a fully-qualified provider
// address with. That rules out two distinct things at once: a host-qualified
// address (app.terraform.io/acme/vpc/aws), which needs the host key inference
// cannot supply, and the forge shorthands Terraform reads as git sources
// (github.com/hashicorp/example, bitbucket.org/org/repo), which are three plain
// segments carrying no other marker of what they are.
func registryAddress(source string) bool {
	// namespace/name/target, so two separators.
	const registrySeparators = 2

	if source == "" ||
		strings.HasPrefix(source, ".") ||
		strings.HasPrefix(source, "/") ||
		strings.Contains(source, "::") ||
		strings.Contains(source, "//") ||
		strings.Count(source, "/") != registrySeparators {
		return false
	}
	namespace, _, _ := strings.Cut(source, "/")
	return !strings.ContainsAny(namespace, ".:")
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
