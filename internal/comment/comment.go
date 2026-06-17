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

// Body returns the comment text on line and whether one was found. For a line
// marker it is the text after the marker; for a block it is the text between
// the open and close delimiters, or everything after the open when the close
// falls on a later line. The earliest-starting delimiter wins, so
// `code // note` yields " note".
//
// Matching is byte-literal: cusp is line-based and does not parse the host
// format, so a delimiter sitting inside a string literal is not distinguished
// from a real comment. The distinctive cusp: keyword keeps false positives
// rare in practice.
func (s Syntax) Body(line string) (string, bool) {
	start, body, found := -1, "", false

	for _, marker := range s.Line {
		i := strings.Index(line, marker)
		if i < 0 || (found && i >= start) {
			continue
		}
		start, body, found = i, line[i+len(marker):], true
	}

	for _, block := range s.Blocks {
		i := strings.Index(line, block.Open)
		if i < 0 || (found && i >= start) {
			continue
		}
		rest := line[i+len(block.Open):]
		if j := strings.Index(rest, block.Close); j >= 0 {
			rest = rest[:j]
		}
		start, body, found = i, rest, true
	}

	return body, found
}
