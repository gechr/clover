package comment

import "strings"

// Block is a paired block-comment delimiter, e.g. {Open: "<!--", Close: "-->"}.
type Block struct {
	Open  string
	Close string
}

// Syntax is the set of comment delimiters a file format recognises. A format
// may expose several line markers (HCL uses both # and //) and several block
// pairs; [Syntax.Body] returns whichever comment starts earliest on the line.
type Syntax struct {
	Line   []string
	Blocks []Block
}

// IsComment reports whether line is wholly a comment - its first non-blank
// token is a comment marker - so a commented-out example is never treated as a
// live target.
func (s Syntax) IsComment(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	for _, marker := range s.Line {
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	for _, block := range s.Blocks {
		if strings.HasPrefix(trimmed, block.Open) {
			return true
		}
	}
	return false
}

// Body returns the comment text on line and whether one was found. For a line
// marker it is the text after the marker; for a block it is the text between
// the open and close delimiters, or everything after the open when the close
// falls on a later line. The earliest-starting delimiter wins, so
// `code // note` yields " note".
//
// Matching is byte-literal: clover is line-based and does not parse the host
// format, so a delimiter sitting inside a string literal is not distinguished
// from a real comment. The distinctive clover: keyword keeps false positives
// rare in practice.
func (s Syntax) Body(line string) (string, bool) {
	m, ok := s.locate(line)
	if !ok {
		return "", false
	}
	return m.body(line), true
}

// body extracts the comment text the match delimits: for a line comment, the
// rest of the line; for a block, the text up to the close delimiter, or
// everything after the open when the close falls on a later line.
func (m match) body(line string) string {
	rest := line[m.start+m.openLen:]
	if m.close == "" || !m.hasClose {
		return rest
	}
	if j := strings.Index(rest, m.close); j >= 0 {
		return rest[:j]
	}
	return rest
}
