package match

import (
	"errors"
	"regexp"
)

// pypiSpecifier matches a quoted PEP 508 dependency specifier whose constraint
// tracking can bump: a project name (with optional extras) followed by a pin
// or floor comparator, with the match ending where the pinned version begins.
// Exclusions (!=) and caps (<, <=, >) are deliberately absent - bumping them
// to the latest version would invert their meaning.
var pypiSpecifier = regexp.MustCompile(
	`["']([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)\s*(?:\[[^\]]*\])?\s*(?:===|==|~=|>=)\s*`,
)

// requirementSpec is one dependency specifier found on a line: the package it
// names and the byte offset where its pinned version begins.
type requirementSpec struct {
	name    string
	version int
}

// requirementSpecs returns the dependency specifiers on the line whose opening
// quote sits where a TOML string value can: at the start of the line (a
// continuation array element) or after =, [, or a comma. A quoted comparator
// inside prose (a description, a comment) fails that guard and is not a
// specifier.
func requirementSpecs(line string) []requirementSpec {
	var specs []requirementSpec
	for _, m := range pypiSpecifier.FindAllStringSubmatchIndex(line, -1) {
		if !requirementContext(line, m[0]) {
			continue
		}
		specs = append(specs, requirementSpec{name: line[m[2]:m[3]], version: m[1]})
	}
	return specs
}

// requirementContext reports whether the quote at i opens a TOML value or
// array element: scanning left, the nearest non-space byte must be =, [, or a
// comma, or the line must hold nothing before the quote.
func requirementContext(line string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		switch line[j] {
		case ' ', '\t':
			continue
		case '=', '[', ',':
			return true
		default:
			return false
		}
	}
	return true
}

// Requirement rewrites the pinned version of a quoted PEP 508 dependency
// specifier in pyproject.toml, e.g. "uv_build>=0.8.24". It anchors on the
// specifier's own version - the token just past the comparator - so another
// version elsewhere on the line (an environment marker's, another entry's) is
// never the one bumped, and it demands the constraint end at that version, so
// a range or a .post suffix is rejected rather than half-rewritten.
type Requirement struct{}

// NewRequirement returns the dependency-specifier rewriter (stateless value,
// like the other format rewriters).
func NewRequirement() Requirement { return Requirement{} }

// Locate finds the single dependency specifier on the line and the version it
// pins. It errors when the line carries no specifier or more than one (a
// single-line group like dev = ["a>=1", "b>=2"] is ambiguous), when the pinned
// version is not version-shaped (a four-component core), when it carries a
// local +tag the rewriter cannot re-render, or when the constraint continues
// past it (a range, a .post suffix, trailing prose).
func (Requirement) Locate(line string) (Location, error) {
	specs := requirementSpecs(line)
	switch len(specs) {
	case 0:
		return nil, errors.New("no dependency specifier on the line")
	case 1:
		// The single specifier anchors the rewrite.
	default:
		return nil, errors.New("multiple dependency specifiers, so it is ambiguous which to track")
	}

	token, ok := tokenAt(line, specs[0].version)
	if !ok {
		return nil, errors.New("the specifier pins no version-shaped version")
	}
	if token.Build != "" {
		return nil, errors.New("a local version pin cannot be re-rendered faithfully")
	}
	if err := requirementTail(line, token.Span.End); err != nil {
		return nil, err
	}
	return locatedToken(line, token), nil
}

// tokenAt returns the version token starting exactly at start, so the located
// version is the specifier's own and never a lookalike elsewhere on the line.
func tokenAt(line string, start int) (Token, bool) {
	for _, token := range Find(line) {
		switch {
		case token.Span.Start == start:
			return token, true
		case token.Span.Start > start:
			return Token{}, false
		}
	}
	return Token{}, false
}

// requirementTail verifies nothing but the closing quote or an environment
// marker follows the pinned version, so a compound constraint (>=1.26,<2.1) or
// a suffix the token does not carry (.post1, a wildcard .*) is rejected rather
// than half-rewritten.
func requirementTail(line string, end int) error {
	for i := end; i < len(line); i++ {
		switch line[i] {
		case ' ', '\t':
			continue
		case '"', '\'', ';':
			return nil
		default:
			return errors.New(
				"the constraint continues past the pinned version, so it is ambiguous",
			)
		}
	}
	return nil
}
