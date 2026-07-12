package sidecar

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/jq"
)

// Locator resolves sidecar entries against one target file's lines. The
// per-file work a jq locator needs - joining the lines, indexing every value's
// byte offset in a single position-aware walk, and decoding the document for
// gojq path derivation - is done once, on first need, and shared by every
// entry. A Locator is not safe for concurrent use.
type Locator struct {
	lines []string

	source []byte // lines rejoined for the byte walks, built on first use

	index    *docIndex
	indexErr error
	indexed  bool

	input    any // decoded document for gojq path derivation
	inputErr error
	decoded  bool
}

// NewLocator returns a Locator resolving entries against lines, the target's
// LF-normalized content.
func NewLocator(lines []string) *Locator {
	return &Locator{lines: lines}
}

// Locate resolves one entry's target line via its locator. An entry must carry
// find or jq (neither present is a hard error). jq resolves the line by JSON
// path - robust against a duplicated version string or reordered keys - and a
// composing find then refines the region within that line at rewrite time;
// find alone scans for the single line whose content matches a glob or /regex/.
func (l *Locator) Locate(d directive.Directive) (int, error) {
	jqExpr, hasJQ := d.Get(constant.DirectiveJQ)
	find, hasFind := d.Get(constant.DirectiveFind)
	switch {
	case hasJQ:
		// jq selects the line; a composing find refines the region within it at
		// rewrite time, since the rewriter reads the resolved line's directive.
		return l.locateJQ(jqExpr)
	case hasFind:
		return locateFind(l.lines, find)
	default:
		return 0, fmt.Errorf(
			"needs a %q or %q locator",
			constant.DirectiveFind,
			constant.DirectiveJQ,
		)
	}
}

// locateJQ resolves a jq locator to a line: the expression is reduced to a
// concrete path, and the path is looked up in the document's offset index.
func (l *Locator) locateJQ(expr string) (int, error) {
	if expr == "" {
		return 0, fmt.Errorf("%q expression is empty", constant.DirectiveJQ)
	}
	idx, err := l.docIndex()
	if err != nil {
		return 0, err
	}
	// A duplicated object key leaves the shadowed member's descendants in the
	// index, so the per-entry byte walk - which follows last-value-wins at each
	// object level - resolves the document instead.
	if idx.legacy {
		return resolveJQLine(l.joined(), expr)
	}

	path, err := l.jqPath(expr)
	if err != nil {
		return 0, err
	}
	off, err := idx.offsetAt(path)
	if err != nil {
		return 0, err
	}
	return lineIndex(idx.starts, off), nil
}

// jqPath reduces a locator expression to the single concrete path it selects,
// by wrapping it in path(...) and running it over the decoded document. Zero
// or many matches are clear errors.
func (l *Locator) jqPath(expr string) ([]any, error) {
	input, err := l.decodedInput()
	if err != nil {
		return nil, err
	}

	code, err := jq.Compile("path(" + expr + ")")
	if err != nil {
		return nil, fmt.Errorf("%q locator: %w", constant.DirectiveJQ, err)
	}

	var paths [][]any
	iter := code.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if runErr, isErr := v.(error); isErr {
			return nil, fmt.Errorf("%q locator: %w", constant.DirectiveJQ, runErr)
		}
		path, isPath := v.([]any)
		if !isPath {
			return nil, fmt.Errorf("%q locator did not return a path", constant.DirectiveJQ)
		}
		paths = append(paths, path)
	}
	switch len(paths) {
	case 0:
		return nil, errors.New("jq matched nothing")
	case 1:
		return paths[0], nil
	default:
		return nil, fmt.Errorf("jq matched %d values - narrow it", len(paths))
	}
}

// joined is the target's lines rejoined into the byte source the walks
// descend, so offsets map back to line indices consistently with scan's split.
func (l *Locator) joined() []byte {
	if l.source == nil {
		l.source = []byte(strings.Join(l.lines, "\n"))
	}
	return l.source
}

// docIndex is the document's byte-offset index, built by a single walk on
// first use.
func (l *Locator) docIndex() (*docIndex, error) {
	if !l.indexed {
		l.indexed = true
		l.index, l.indexErr = indexDocument(l.joined())
	}
	return l.index, l.indexErr
}

// decodedInput is the document decoded for gojq, built on first use.
func (l *Locator) decodedInput() (any, error) {
	if !l.decoded {
		l.decoded = true
		if err := json.Unmarshal(l.joined(), &l.input); err != nil {
			l.inputErr = jsonTargetError(err)
		}
	}
	return l.input, l.inputErr
}

// jsonTargetError frames a parse failure as the jq locator's non-JSON-target
// error.
func jsonTargetError(err error) error {
	return fmt.Errorf("%q locator requires a JSON target: %w", constant.DirectiveJQ, err)
}

// canonicalJSONError reports source's parse failure in json.Unmarshal's own
// words, falling back to the walk's error for the (unreachable) case where a
// re-parse succeeds.
func canonicalJSONError(source []byte, walkErr error) error {
	var v any
	return cmp.Or(json.Unmarshal(source, &v), walkErr)
}
