package sidecar

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// Leaf is a string-valued scalar in a JSON document, the unit annotate proposes
// a sidecar entry from: the object key that holds it (the last path segment),
// its value, the 0-based line it sits on, and a jq expression that resolves
// uniquely to that line. Only leaves whose locator round-trips - jq resolves
// back to the leaf's own line, and nowhere else - are returned, so a generated
// locator is robust by construction.
type Leaf struct {
	Key   string
	Value string
	Line  int
	JQ    string
}

// Leaves enumerates the string-valued scalars in a JSON document, each paired
// with a verified jq locator, in line order. It is the recognition half of
// annotate's sidecar generation: the caller decides which leaves name a
// trackable reference. Source that does not parse as JSON yields an error, since
// only a JSON target carries a jq-locatable structure. A leaf held under an
// array index (no object key), or one whose derived locator does not round-trip,
// is dropped rather than guessed at.
func Leaves(source []byte) ([]Leaf, error) {
	var input any
	if err := json.Unmarshal(source, &input); err != nil {
		return nil, err
	}

	var paths [][]any
	collectStringPaths(input, nil, &paths)

	leaves := make([]Leaf, 0, len(paths))
	for _, path := range paths {
		key, ok := lastKey(path)
		if !ok {
			continue // an array element has no object key to track by
		}
		value, ok := stringAt(input, path)
		if !ok {
			continue
		}
		expr := pathToJQ(path)
		// Round-trip the derived locator through the same resolver run uses: it
		// must resolve to exactly one line, and that line must be the leaf's own,
		// so a serialization quirk can never emit a locator that points elsewhere.
		line, err := resolveJQLine(source, expr)
		if err != nil {
			continue
		}
		off, err := valueOffset(source, path)
		if err != nil || bytes.Count(source[:off], newline) != line {
			continue
		}
		leaves = append(leaves, Leaf{Key: key, Value: value, Line: line, JQ: expr})
	}
	slices.SortFunc(leaves, func(a, b Leaf) int { return a.Line - b.Line })
	return leaves, nil
}

// collectStringPaths walks the decoded JSON, appending the path to every
// string-valued scalar it reaches. Object keys and array indices are recorded as
// path segments, mirroring the segments [valueOffset] descends.
func collectStringPaths(v any, prefix []any, out *[][]any) {
	switch t := v.(type) {
	case map[string]any:
		for key, child := range t {
			collectStringPaths(child, appendSegment(prefix, key), out)
		}
	case []any:
		for i, child := range t {
			collectStringPaths(child, appendSegment(prefix, i), out)
		}
	case string:
		*out = append(*out, prefix)
	}
}

// appendSegment returns a fresh slice with seg appended, never aliasing prefix,
// so each recursion keeps its own path.
func appendSegment(prefix []any, seg any) []any {
	next := make([]any, len(prefix)+1)
	copy(next, prefix)
	next[len(prefix)] = seg
	return next
}

// stringAt walks the decoded JSON along path and returns the string it ends on.
// ok is false when the path does not lead to a string value.
func stringAt(v any, path []any) (string, bool) {
	cur := v
	for _, seg := range path {
		switch s := seg.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				return "", false
			}
			cur, ok = m[s]
			if !ok {
				return "", false
			}
		case int:
			a, ok := cur.([]any)
			if !ok || s < 0 || s >= len(a) {
				return "", false
			}
			cur = a[s]
		}
	}
	str, ok := cur.(string)
	return str, ok
}

// lastKey returns the object key a path ends on, or false when the final segment
// is an array index (which names no key to track by).
func lastKey(path []any) (string, bool) {
	if len(path) == 0 {
		return "", false
	}
	key, ok := path[len(path)-1].(string)
	return key, ok
}

// pathToJQ serializes a path into a jq expression in bracket form
// (.["a"]["b"][0]), which is robust for any object key - including one with a
// leading $ or a dot - so the emitted locator never needs hand-quoting.
func pathToJQ(path []any) string {
	var b strings.Builder
	b.WriteByte('.')
	for _, seg := range path {
		b.WriteByte('[')
		switch s := seg.(type) {
		case string:
			b.WriteString(strconv.Quote(s))
		case int:
			b.WriteString(strconv.Itoa(s))
		}
		b.WriteByte(']')
	}
	return b.String()
}
