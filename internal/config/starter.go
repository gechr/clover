package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// exampleConstraint is shown as a commented placeholder when init writes a
// config without a required-version.
const exampleConstraint = ">=0.1.0"

// defaultExcludes are the globs a fresh config excludes from scanning: the
// directories that usually hold vendored or fixture versions clover should not
// manage. They are written active so a new project starts with sane scoping.
var defaultExcludes = []string{"vendor/**", "**/testdata/**"}

// Starter renders a commented starter .clover.yaml. A non-empty requiredVersion
// becomes an active required-version constraint; an empty one is shown as a
// commented example, documenting the field without imposing a gate. The output
// carries a yaml-language-server modeline so editors validate it against the
// published schema, and round-trips cleanly through [Load].
func Starter(requiredVersion string) []byte {
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
	b.WriteString("paths:\n")
	b.WriteString("  exclude:\n")
	for _, glob := range defaultExcludes {
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
