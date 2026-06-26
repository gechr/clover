// Package tag parses and evaluates the --tag marker filter. A marker carries
// its own labels (tags=prod,ci); a filter built from one or more --tag values
// decides which markers a run touches. Within a single --tag value, "/"
// separates OR alternatives and "," separates AND requirements, and a "!" prefix
// excludes a tag; repeated values accumulate. The filter is pure and the
// rendering is human-readable, so the CLI can log exactly what it will act on.
package tag

import (
	"fmt"
	"strings"

	xstrings "github.com/gechr/x/strings"
)

// orSeparator splits a --tag value into OR alternatives; andSeparator splits it
// into AND requirements. A value is read as OR when it contains a slash, else as
// AND, so prod/ci means "prod or ci" and prod,ci means "prod and ci". A term
// carrying notPrefix is an exclusion regardless of the separator.
const (
	orSeparator  = "/"
	andSeparator = ","
	notPrefix    = "!"
)

// Tokens of the human-readable expression rendered by [Filter.String].
const (
	logicalAnd = " AND "
	logicalOr  = " OR "
	logicalNot = "NOT "
	parenOpen  = "("
	parenClose = ")"
)

// Filter selects markers by tag. A marker matches when it carries none of Not
// (exclusions veto), every tag in All (AND), and at least one tag in Any (OR).
// An empty list drops that requirement, so the zero Filter matches everything.
// Matching is case-insensitive.
type Filter struct {
	All []string
	Any []string
	Not []string
}

// Parse builds a Filter from raw --tag values. A value containing "/" splits on
// it into OR alternatives; any other value splits on "," into AND requirements.
// A term prefixed with "!" is an exclusion, collected regardless of separator.
// Surrounding whitespace is trimmed and empty items dropped; repeated flags
// accumulate, so --tag a,b --tag c/d yields All=[a b], Any=[c d].
//
// Mixing "," and "/" in a single value is ambiguous (AND-vs-OR precedence) and
// is rejected, rather than silently producing an unmatchable term; combine an
// AND set and an OR set by passing them as separate --tag values instead.
func Parse(values []string) (Filter, error) {
	var f Filter
	for _, value := range values {
		hasOr := strings.Contains(value, orSeparator)
		hasAnd := strings.Contains(value, andSeparator)
		if hasOr && hasAnd {
			return Filter{}, fmt.Errorf(
				"tag filter %q mixes %q (and) with %q (or); use one per --tag value",
				value, andSeparator, orSeparator,
			)
		}

		sep := andSeparator
		if hasOr {
			sep = orSeparator
		}
		for _, term := range xstrings.SplitBy(value, sep) {
			switch {
			case strings.HasPrefix(term, notPrefix):
				if excluded := strings.TrimSpace(term[len(notPrefix):]); excluded != "" {
					f.Not = append(f.Not, excluded)
				}
			case hasOr:
				f.Any = append(f.Any, term)
			default:
				f.All = append(f.All, term)
			}
		}
	}
	return f, nil
}

// Empty reports whether the filter constrains nothing, in which case every
// marker matches.
func (f Filter) Empty() bool {
	return len(f.All) == 0 && len(f.Any) == 0 && len(f.Not) == 0
}

// Match reports whether a marker carrying tags satisfies the filter. An empty
// filter matches everything. An exclusion vetoes a marker carrying that tag;
// an include requirement (All/Any) is never satisfied by an untagged marker, so
// a filter with includes targets exactly the tagged markers, while a
// pure-exclusion filter keeps everything except the excluded.
func (f Filter) Match(tags []string) bool {
	for _, excluded := range f.Not {
		if contains(tags, excluded) {
			return false
		}
	}
	for _, want := range f.All {
		if !contains(tags, want) {
			return false
		}
	}
	if len(f.Any) == 0 {
		return true
	}
	for _, want := range f.Any {
		if contains(tags, want) {
			return true
		}
	}
	return false
}

// String renders the filter as a boolean expression, e.g. "prod AND ci",
// "eu OR us", "(prod AND ci) AND (eu OR us)", or "prod AND NOT legacy". The zero
// Filter renders empty.
func (f Filter) String() string {
	groups := len(f.Not)
	if len(f.All) > 0 {
		groups++
	}
	if len(f.Any) > 0 {
		groups++
	}

	var parts []string
	if len(f.All) > 0 {
		allPart := strings.Join(f.All, logicalAnd)
		if len(f.All) > 1 && groups > 1 {
			allPart = parenOpen + allPart + parenClose
		}
		parts = append(parts, allPart)
	}
	if len(f.Any) > 0 {
		anyPart := strings.Join(f.Any, logicalOr)
		if len(f.Any) > 1 && groups > 1 {
			anyPart = parenOpen + anyPart + parenClose
		}
		parts = append(parts, anyPart)
	}
	for _, excluded := range f.Not {
		parts = append(parts, logicalNot+excluded)
	}
	return strings.Join(parts, logicalAnd)
}

// contains reports whether tags holds want, case-insensitively.
func contains(tags []string, want string) bool {
	for _, tag := range tags {
		if strings.EqualFold(tag, want) {
			return true
		}
	}
	return false
}
