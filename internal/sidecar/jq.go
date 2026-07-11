package sidecar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/jq"
)

// newline is the byte an offset is counted against to find its line index,
// matching scan's LF-normalized line split.
var newline = []byte{'\n'}

// resolveJQLine resolves a jq locator to the 0-based line of the value it
// selects. gojq discards byte positions when it parses JSON, so it is used only
// to compute a path: the user's expression is wrapped in path(...), run once, and
// must yield exactly one path. An independent position-aware byte walk then
// follows that path to the value's offset - the JSON is never re-serialized, so
// formatting and key order are preserved. A non-JSON target, a non-path
// expression, or zero/many matches are clear errors.
func resolveJQLine(source []byte, expr string) (int, error) {
	var input any
	if err := json.Unmarshal(source, &input); err != nil {
		return 0, fmt.Errorf("%q locator requires a JSON target: %w", constant.DirectiveJQ, err)
	}

	code, err := jq.Compile("path(" + expr + ")")
	if err != nil {
		return 0, fmt.Errorf("%q locator: %w", constant.DirectiveJQ, err)
	}

	var paths [][]any
	iter := code.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if runErr, isErr := v.(error); isErr {
			return 0, fmt.Errorf("%q locator: %w", constant.DirectiveJQ, runErr)
		}
		path, isPath := v.([]any)
		if !isPath {
			return 0, fmt.Errorf("%q locator did not return a path", constant.DirectiveJQ)
		}
		paths = append(paths, path)
	}
	switch len(paths) {
	case 0:
		return 0, errors.New("jq matched nothing")
	case 1:
	default:
		return 0, fmt.Errorf("jq matched %d values - narrow it", len(paths))
	}

	off, err := valueOffset(source, paths[0])
	if err != nil {
		return 0, err
	}
	return bytes.Count(source[:off], newline), nil
}

// errJQStructure reports a path that does not match the JSON shape; it cannot
// arise from a path gojq derived from the same document.
var errJQStructure = fmt.Errorf(
	"%q locator path does not match the JSON structure",
	constant.DirectiveJQ,
)

// valueOffset walks the raw JSON along path and returns the byte offset where the
// located value begins (its first non-whitespace byte, which for a value placed
// below its key is the value's own line, not the key's). At each object segment
// the LAST member matching the key is chosen, mirroring the last-value-wins
// semantics gojq and encoding/json use for a duplicated key, so the resolved line
// is the one gojq's path actually refers to. A negative array index counts from
// the end, as in jq.
func valueOffset(source []byte, path []any) (int, error) {
	pos := 0
	for _, seg := range path {
		next, err := childOffset(source, pos, seg)
		if err != nil {
			return 0, err
		}
		pos = next
	}
	return pos, nil
}

// childOffset, given pos at the start of a container value, returns the byte
// offset where the child named by seg begins: the last member matching an object
// key, or the element at an array index (negative counts from the end). The path
// comes from gojq running over this same JSON, so a structural mismatch is a
// should-not-happen consistency error.
func childOffset(source []byte, pos int, seg any) (int, error) {
	dec := json.NewDecoder(bytes.NewReader(source[pos:]))
	open, err := dec.Token()
	if err != nil {
		return 0, err
	}
	delim, ok := open.(json.Delim)
	if !ok {
		return 0, errJQStructure
	}

	switch delim {
	case '{':
		key, ok := seg.(string)
		if !ok {
			return 0, errJQStructure
		}
		chosen := -1
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return 0, err
			}
			start := skipToValue(source, pos+int(dec.InputOffset()))
			if tok == key {
				chosen = start // last-wins: a duplicated key resolves to its final member
			}
			if err := skipValue(dec); err != nil {
				return 0, err
			}
		}
		if chosen < 0 {
			return 0, fmt.Errorf("%q locator key %q not found", constant.DirectiveJQ, key)
		}
		return chosen, nil
	case '[':
		idx, ok := toIndex(seg)
		if !ok {
			return 0, errJQStructure
		}
		var starts []int
		cursor := pos + int(dec.InputOffset())
		for dec.More() {
			starts = append(starts, skipToValue(source, cursor))
			if err := skipValue(dec); err != nil {
				return 0, err
			}
			cursor = pos + int(dec.InputOffset())
		}
		at := idx
		if at < 0 {
			at += len(starts)
		}
		if at < 0 || at >= len(starts) {
			return 0, fmt.Errorf("%q locator index %d out of range", constant.DirectiveJQ, idx)
		}
		return starts[at], nil
	default:
		return 0, errJQStructure
	}
}

// skipToValue advances past JSON whitespace and a single separator - the ':' after
// an object key or the ',' between array elements - to the first byte of the value.
func skipToValue(source []byte, off int) int {
	for off < len(source) && isJSONSpace(source[off]) {
		off++
	}
	if off < len(source) && (source[off] == ':' || source[off] == ',') {
		off++
		for off < len(source) && isJSONSpace(source[off]) {
			off++
		}
	}
	return off
}

// isJSONSpace reports whether b is one of JSON's four insignificant whitespace
// bytes.
func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// skipValue consumes one complete JSON value, descending through nested objects
// and arrays so the decoder lands just after it.
func skipValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return nil // a scalar is a single token
	}
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch tok {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
	return nil
}

// toIndex reads an array index from a path segment. gojq emits indices as int,
// but a float64 is accepted in case a JSON-number path slips through.
func toIndex(seg any) (int, bool) {
	switch n := seg.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
