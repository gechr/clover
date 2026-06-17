package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// exampleConstraint is shown as a commented placeholder when init writes a
// config without a required-version.
const exampleConstraint = ">=0.1.0"

// exampleExclude is shown as a commented placeholder when init writes a config
// with no excludes selected.
const exampleExclude = "vendor/**"

// commonExcludes are the globs the init wizard offers as exclusion choices: the
// directories that usually hold vendored, generated, or fixture versions clover
// should not manage.
var commonExcludes = []string{
	"vendor/**",
	"**/testdata/**",
	"**/node_modules/**",
	"dist/**",
	"build/**",
}

// defaultExcludes are the subset of [CommonExcludes] preselected in the wizard.
var defaultExcludes = []string{"vendor/**", "**/testdata/**"}

// CommonExcludes returns the exclude globs the init wizard offers, as a fresh
// copy.
func CommonExcludes() []string { return slices.Clone(commonExcludes) }

// DefaultExcludes returns the exclude globs preselected by the init wizard, as a
// fresh copy.
func DefaultExcludes() []string { return slices.Clone(defaultExcludes) }

// Starter renders a commented starter .clover.yaml. A non-empty requiredVersion
// becomes an active required-version constraint; an empty one is shown as a
// commented example, documenting the field without imposing a gate. Likewise a
// non-empty excludes becomes an active paths.exclude block; an empty one is
// shown commented, so the output never carries a null array. The output carries
// a yaml-language-server modeline so editors validate it against the published
// schema, and round-trips cleanly through [Load].
func Starter(requiredVersion string, excludes []string) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "# yaml-language-server: $schema=%s\n\n", schemaURL())

	b.WriteString("# The minimum clover version this project requires. clover refuses to run\n")
	b.WriteString("# when its own version does not satisfy this constraint.\n")
	if requiredVersion == "" {
		fmt.Fprintf(&b, "# required-version: %s\n\n", strconv.Quote(exampleConstraint))
	} else {
		fmt.Fprintf(&b, "required-version: %s\n\n", strconv.Quote(requiredVersion))
	}

	b.WriteString("# Paths excluded from scanning, as doublestar globs.\n")
	if len(excludes) == 0 {
		b.WriteString("# paths:\n#   exclude:\n")
		fmt.Fprintf(&b, "#     - %s\n", strconv.Quote(exampleExclude))
		return []byte(b.String())
	}
	b.WriteString("paths:\n  exclude:\n")
	for _, glob := range excludes {
		fmt.Fprintf(&b, "    - %s\n", strconv.Quote(glob))
	}

	return []byte(b.String())
}

// schemaURL returns the published location of the config schema, read from the
// embedded document's $id so the modeline and the schema stay in lockstep. The
// embedded schema is validated at package init (see compileSchema), so the
// unmarshal here cannot fail in practice.
func schemaURL() string {
	var doc struct {
		ID string `json:"$id"`
	}
	_ = json.Unmarshal(schemaJSON, &doc)
	return doc.ID
}
