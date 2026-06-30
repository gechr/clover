package sidecar

import (
	"bytes"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Leaf is a string-valued scalar in a JSON document, the unit annotate proposes
// a sidecar entry from: the object key that holds it (the last path segment,
// empty for array elements), its value, the 0-based line it sits on, and a jq
// expression that resolves uniquely to that line. Only leaves whose locator
// round-trips - jq resolves back to the leaf's own line, and nowhere else - are
// returned, so a generated locator is robust by construction.
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
// only a JSON target carries a jq-locatable structure. A leaf whose locator the
// resolver would point elsewhere is dropped - an earlier member of a duplicated
// key, or a leaf below a shadowed duplicate ancestor.
//
// A single position-aware descent records every string scalar's path and byte
// offset at once, so the cost is linear in the document size rather than the
// per-leaf re-parse-and-re-walk a naive enumeration would pay.
func Leaves(source []byte) ([]Leaf, error) {
	var input any
	if err := json.Unmarshal(source, &input); err != nil {
		return nil, err
	}

	var raw []rawLeaf
	if err := walkLeaves(json.NewDecoder(bytes.NewReader(source)), source, nil, &raw); err != nil {
		return nil, err
	}

	lines := lineStarts(source)

	// Keyed by locator so a duplicated path keeps only the member the resolver
	// selects: the parsed structure is last-value-wins, so a raw leaf survives
	// only when input resolves its path back to the same string. An earlier
	// duplicate, or a leaf under a shadowed duplicate ancestor, fails that check.
	byExpr := make(map[string]keptLeaf, len(raw))
	for _, r := range raw {
		key, _ := lastKey(r.path)
		if v, ok := stringAt(input, r.path); !ok || v != r.value {
			continue
		}
		expr := pathToJQ(r.path)
		byExpr[expr] = keptLeaf{
			off: r.off,
			leaf: Leaf{
				Key:   key,
				Value: r.value,
				Line:  lineIndex(lines, r.off),
				JQ:    expr,
			},
		}
	}

	// Emit in document (byte-offset) order, so two leaves sharing a line keep a
	// stable order - their map iteration order is not deterministic, and sorting
	// by line alone would leave same-line ties to chance.
	kept := make([]keptLeaf, 0, len(byExpr))
	for _, k := range byExpr {
		kept = append(kept, k)
	}
	slices.SortFunc(kept, func(a, b keptLeaf) int { return a.off - b.off })

	leaves := make([]Leaf, len(kept))
	for i, k := range kept {
		leaves[i] = k.leaf
	}
	return leaves, nil
}

// lineStarts returns the byte offset where each line starts, beginning with 0.
func lineStarts(source []byte) []int {
	starts := []int{0}
	for i, b := range source {
		if b == newline[0] {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// lineIndex maps a byte offset to its 0-based line number using line starts.
func lineIndex(starts []int, off int) int {
	return max(0, sort.Search(len(starts), func(i int) bool { return starts[i] > off })-1)
}

// rawLeaf is one string scalar as the byte walk found it: the path of object keys
// and array indices reaching it, its value, and the byte offset its value begins
// at.
type rawLeaf struct {
	path  []any
	value string
	off   int
}

// keptLeaf pairs a surviving leaf with its byte offset, so the result can be put
// back into document order after the map-keyed deduplication.
type keptLeaf struct {
	leaf Leaf
	off  int
}

// walkLeaves consumes the single JSON value the decoder is positioned before,
// recording every string scalar reached with its path and the byte offset of its
// value. It descends objects and arrays in one pass, so each value is tokenized
// exactly once.
func walkLeaves(dec *json.Decoder, source []byte, path []any, out *[]rawLeaf) error {
	pre := int(dec.InputOffset())
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyTok.(string)
				if !ok {
					return errJQStructure
				}
				if err := walkLeaves(dec, source, appendSegment(path, key), out); err != nil {
					return err
				}
			}
		case '[':
			for i := 0; dec.More(); i++ {
				if err := walkLeaves(dec, source, appendSegment(path, i), out); err != nil {
					return err
				}
			}
		}
		_, err := dec.Token() // consume the closing } or ]
		return err
	case string:
		// pre sits just past the preceding token; skipToValue advances over the
		// key's ':' (or an element ','), landing on the value's own first byte.
		*out = append(*out, rawLeaf{path: path, value: t, off: skipToValue(source, pre)})
	}
	return nil
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
			b.WriteString(jqString(s))
		case int:
			b.WriteString(strconv.Itoa(s))
		}
		b.WriteByte(']')
	}
	return b.String()
}

// jqString quotes s as a JSON string literal, the form jq's lexer accepts
// verbatim. strconv.Quote would emit Go-only escapes (\x, \U) that jq rejects, so
// a key with a control character would otherwise yield a locator that cannot be
// parsed back at apply time.
func jqString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string never fails to encode; Encode appends a newline
	return strings.TrimSuffix(b.String(), "\n")
}
