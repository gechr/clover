// Package tag parses and evaluates the --tags marker filter. A marker carries
// its own labels (tags=prod,ci); a filter built from one or more --tags values
// decides which markers a run touches. Within a single --tags value, "/"
// separates OR alternatives and "," separates AND requirements; repeated values
// accumulate. The filter is pure and the rendering is human-readable, so the CLI
// can log exactly what it will act on.
package tag

import "strings"

// orSeparator splits a --tags value into OR alternatives; andSeparator splits it
// into AND requirements. A value is read as OR when it contains a slash, else as
// AND, so prod/ci means "prod or ci" and prod,ci means "prod and ci".
const (
	orSeparator  = "/"
	andSeparator = ","
)

// Filter selects markers by tag. A marker matches when it carries every tag in
// All (AND) and at least one tag in Any (OR). Either list being empty drops that
// requirement, so the zero Filter matches everything. Matching is
// case-insensitive.
type Filter struct {
	All []string
	Any []string
}

// Parse builds a Filter from raw --tags values. A value containing "/" splits on
// it into OR alternatives; any other value splits on "," into AND requirements.
// Surrounding whitespace is trimmed and empty items dropped; repeated flags
// accumulate, so --tags a,b --tags c/d yields All=[a b], Any=[c d].
func Parse(values []string) Filter {
	var f Filter
	for _, value := range values {
		if strings.Contains(value, orSeparator) {
			f.Any = append(f.Any, split(value, orSeparator)...)
		} else {
			f.All = append(f.All, split(value, andSeparator)...)
		}
	}
	return f
}

// Empty reports whether the filter constrains nothing, in which case every
// marker matches.
func (f Filter) Empty() bool {
	return len(f.All) == 0 && len(f.Any) == 0
}

// Match reports whether a marker carrying tags satisfies the filter. An empty
// filter matches everything; a marker with no tags never matches a non-empty
// filter, so --tags targets exactly the tagged markers.
func (f Filter) Match(tags []string) bool {
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
// "eu OR us", or "(prod AND ci) AND (eu OR us)". The zero Filter renders empty.
func (f Filter) String() string {
	var parts []string
	if len(f.All) > 0 {
		allPart := strings.Join(f.All, " AND ")
		if len(f.All) > 1 && len(f.Any) > 0 {
			allPart = "(" + allPart + ")"
		}
		parts = append(parts, allPart)
	}
	if len(f.Any) > 0 {
		anyPart := strings.Join(f.Any, " OR ")
		if len(f.Any) > 1 && len(f.All) > 0 {
			anyPart = "(" + anyPart + ")"
		}
		parts = append(parts, anyPart)
	}
	return strings.Join(parts, " AND ")
}

// split divides value on sep, trimming each item and dropping empties.
func split(value, sep string) []string {
	var out []string
	for item := range strings.SplitSeq(value, sep) {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
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
