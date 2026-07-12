package sidecar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/gechr/clover/internal/constant"
)

// docIndex is a one-pass position index of a JSON document: every value's
// byte offset keyed by its canonical jq path, each array's length for
// negative-index resolution, and the line starts that map offsets to lines.
// Resolving a locator against it is a lookup, so a sidecar with many entries
// tokenizes its target once instead of once per entry per path segment.
type docIndex struct {
	values map[string]docValue
	starts []int
	// legacy marks a document with a duplicated object key, whose shadowed
	// descendants the index cannot distinguish from live ones - the per-entry
	// byte walk resolves such a document instead.
	legacy bool
}

// docValue is one indexed value: the byte offset its first byte sits at, its
// container kind ('{' or '[', 0 for a scalar), and, for an array, its length.
type docValue struct {
	off  int
	n    int
	kind byte
}

// indexDocument walks source once, recording every value under its canonical
// jq path. A source that does not parse as JSON - including trailing content
// after the top-level value - is the locator's non-JSON-target error.
func indexDocument(source []byte) (*docIndex, error) {
	idx := &docIndex{
		values: make(map[string]docValue),
		starts: lineStarts(source),
	}
	dec := json.NewDecoder(bytes.NewReader(source))
	if err := idx.walk(dec, source, "."); err != nil {
		return nil, jsonTargetError(canonicalJSONError(source, err))
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, jsonTargetError(canonicalJSONError(source,
			errors.New("unexpected content after top-level value")))
	}
	return idx, nil
}

// walk consumes the single JSON value the decoder is positioned before,
// recording it (and, recursively, every value inside it) under key, its
// canonical jq path.
func (d *docIndex) walk(dec *json.Decoder, source []byte, key string) error {
	pre := int(dec.InputOffset())
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	// pre sits just past the preceding token; skipToValue advances over the
	// key's ':' (or an element ','), landing on the value's own first byte.
	val := docValue{off: skipToValue(source, pre)}

	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		d.record(key, val)
		return nil
	}
	switch delim {
	case '{':
		val.kind = '{'
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return err
			}
			k, ok := keyTok.(string)
			if !ok {
				return errJQStructure
			}
			if err := d.walk(dec, source, key+"["+jqString(k)+"]"); err != nil {
				return err
			}
		}
	case '[':
		val.kind = '['
		for ; dec.More(); val.n++ {
			if err := d.walk(dec, source, key+"["+strconv.Itoa(val.n)+"]"); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil { // consume the closing } or ]
		return err
	}
	d.record(key, val)
	return nil
}

// record stores a value under its path, flagging the document legacy when the
// path was already taken - the signature of a duplicated object key.
func (d *docIndex) record(key string, val docValue) {
	if _, exists := d.values[key]; exists {
		d.legacy = true
	}
	d.values[key] = val
}

// offsetAt resolves a gojq-derived path to its value's byte offset, walking
// the index segment by segment so a miss reports the same error the byte walk
// did: the missing key, the out-of-range index, or a structural mismatch. A
// negative array index counts from the end, as in jq.
func (d *docIndex) offsetAt(path []any) (int, error) {
	key := "."
	for _, seg := range path {
		parent, ok := d.values[key]
		if !ok {
			return 0, errJQStructure
		}
		switch s := seg.(type) {
		case string:
			if parent.kind != '{' {
				return 0, errJQStructure
			}
			key += "[" + jqString(s) + "]"
			if _, ok := d.values[key]; !ok {
				return 0, fmt.Errorf(
					"%q locator key %q not found",
					constant.DirectiveJQ, s,
				)
			}
		default:
			idx, ok := toIndex(seg)
			if !ok || parent.kind != '[' {
				return 0, errJQStructure
			}
			at := idx
			if at < 0 {
				at += parent.n
			}
			if at < 0 || at >= parent.n {
				return 0, fmt.Errorf(
					"%q locator index %d out of range",
					constant.DirectiveJQ, idx,
				)
			}
			key += "[" + strconv.Itoa(at) + "]"
		}
	}
	v, ok := d.values[key]
	if !ok {
		return 0, errJQStructure
	}
	return v.off, nil
}
