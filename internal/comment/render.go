package comment

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
)

// sep is the single space between a comment marker and its body (and around a
// block's close), sourced from the one directive separator so a synthesised or
// rewritten comment spaces exactly like a rendered directive.
const sep = string(constant.DirectiveSeparator)

// match is the comment delimiter found on a line: where it starts, the length of
// its opening marker, and its closing delimiter (empty for a line comment).
type match struct {
	start    int
	openLen  int
	close    string
	hasClose bool // the close delimiter is present on this line
}

// Render returns line with the body of its earliest comment replaced by body,
// preserving the text before the comment and the delimiters, and normalising
// spacing to a single space after the opening marker (and before a block's
// closing marker). ok is false when line carries no comment. It is the inverse
// of [Syntax.Body], used by format mode to rewrite a canonicalised directive
// back onto its line.
func (s Syntax) Render(line, body string) (string, bool) {
	m, ok := s.locate(line)
	if !ok {
		return line, false
	}

	rendered := line[:m.start+m.openLen] + sep + body
	if m.close == "" {
		return rendered, true // line comment: the directive runs to end of line
	}
	if !m.hasClose {
		return rendered, true // block opened here but closed on a later line
	}
	return rendered + sep + m.close + m.afterClose(line), true
}

// Comment builds a fresh comment line carrying body, prefixed by indent, for a
// file that does not yet have a comment on the line. It uses the syntax's first
// line marker, falling back to its first block pair for block-only formats (HTML,
// CSS). ok is false when the syntax exposes no delimiter at all. Spacing matches
// [Syntax.Render]: a single space after the opening marker (and before a block's
// close), so a synthesised comment is byte-identical to a rendered one.
func (s Syntax) Comment(indent, body string) (string, bool) {
	switch {
	case len(s.Line) > 0:
		return indent + s.Line[0] + sep + body, true
	case len(s.Blocks) > 0:
		b := s.Blocks[0]
		return indent + b.Open + sep + body + sep + b.Close, true
	default:
		return "", false
	}
}

// afterClose returns the text following the block's closing delimiter, preserved
// verbatim so trailing content survives a rewrite.
func (m match) afterClose(line string) string {
	rest := line[m.start+m.openLen:]
	if j := strings.Index(rest, m.close); j >= 0 {
		return rest[j+len(m.close):]
	}
	return ""
}

// locate finds the earliest comment on line, mirroring [Syntax.Body]'s
// delimiter precedence so Render targets exactly the comment Body read.
func (s Syntax) locate(line string) (match, bool) {
	var (
		best  match
		found bool
	)

	for _, marker := range s.Line {
		i := strings.Index(line, marker)
		if i < 0 || (found && i >= best.start) {
			continue
		}
		best, found = match{start: i, openLen: len(marker)}, true
	}

	for _, block := range s.Blocks {
		i := strings.Index(line, block.Open)
		if i < 0 || (found && i >= best.start) {
			continue
		}
		rest := line[i+len(block.Open):]
		best = match{
			start:    i,
			openLen:  len(block.Open),
			close:    block.Close,
			hasClose: strings.Contains(rest, block.Close),
		}
		found = true
	}

	return best, found
}
