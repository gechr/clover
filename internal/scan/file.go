package scan

import (
	"bytes"
	"io"
	"os"
	"strings"

	"github.com/gechr/clover/internal/comment"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
)

const prefilterChunkSize = 32 << 10

var directiveKeywordBytes = []byte(constant.DirectiveKeyword)

// Located is a directive found on a line of a file.
type Located struct {
	Line      int // index into File.Lines of the directive's comment line
	Directive directive.Directive
}

// LineError is a malformed directive: the keyword was present but parsing
// failed. lint surfaces these; run skips them.
type LineError struct {
	Line int
	Err  error
}

// File is a scanned file that carries at least one directive. Lines is the
// file's content, split on newlines and retained so the apply phase can rewrite
// in place.
type File struct {
	Path   string
	Lines  []string
	Found  []Located
	Errors []LineError
}

// scanFile reads path and extracts its directives. ok is false when the file is
// missing, too large, binary, or carries no directive at all.
func scanFile(path string, size, maxSize int64) (File, bool) {
	if size < 0 {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			return File{}, false
		}
		size = info.Size()
	}
	if size > maxSize {
		return File{}, false
	}

	if !maybeTextWithDirective(path) {
		return File{}, false
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return File{}, false
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return File{}, false // binary
	}

	// Split losslessly so line numbers and content survive for the rewrite;
	// CRLF is normalised to LF.
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	syntax := comment.For(path)

	var (
		found      []Located
		problems   []LineError
		inBlock    bool
		ignoreLine = -1 // line index suppressed by a preceding clover:ignore
	)
	for i, line := range lines {
		if !strings.Contains(line, constant.DirectiveKeyword) {
			continue // cheap prefilter: most lines have no keyword
		}
		body, ok := syntax.Body(line)
		if !ok {
			continue
		}

		switch directive.ParseIgnore(body) {
		case directive.IgnoreFile:
			return File{}, false // the whole file opts out
		case directive.IgnoreBlockStart:
			inBlock = true
			continue
		case directive.IgnoreBlockEnd:
			inBlock = false
			continue
		case directive.IgnoreNextLine:
			ignoreLine = i + 1
			continue
		case directive.IgnoreNone:
		}
		if inBlock || i == ignoreLine {
			continue // suppressed by a clover:ignore control
		}

		d, ok, err := directive.Parse(body)
		switch {
		case err != nil:
			problems = append(problems, LineError{Line: i, Err: err})
		case ok:
			found = append(found, Located{Line: i, Directive: d})
		}
	}

	if len(found) == 0 && len(problems) == 0 {
		return File{}, false
	}
	return File{Path: path, Lines: lines, Found: found, Errors: problems}, true
}

// maybeTextWithDirective rejects obvious misses before allocating a whole-file
// buffer: files with no clover keyword, and binary files with a NUL byte.
func maybeTextWithDirective(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, prefilterChunkSize)
	tail := make([]byte, 0, len(directiveKeywordBytes)-1)
	foundKeyword := false
	for {
		n, err := file.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if bytes.IndexByte(chunk, 0) >= 0 {
				return false
			}
			if !foundKeyword {
				foundKeyword = containsKeyword(tail, chunk)
				tail = carry(chunk)
			}
		}
		if err == io.EOF {
			return foundKeyword
		}
		if err != nil {
			return false
		}
	}
}

// containsKeyword reports whether chunk contains the directive keyword, either
// wholly inside chunk or split across the previous buffer's tail.
func containsKeyword(tail, chunk []byte) bool {
	if bytes.Contains(chunk, directiveKeywordBytes) {
		return true
	}
	if len(tail) == 0 {
		return false
	}
	prefixLen := min(len(chunk), len(directiveKeywordBytes)-1)
	window := make([]byte, 0, len(tail)+prefixLen)
	window = append(window, tail...)
	window = append(window, chunk[:prefixLen]...)
	return bytes.Contains(window, directiveKeywordBytes)
}

// carry keeps enough trailing bytes to match a directive keyword split across
// two read buffers.
func carry(buf []byte) []byte {
	keep := min(len(buf), len(directiveKeywordBytes)-1)
	out := make([]byte, keep)
	copy(out, buf[len(buf)-keep:])
	return out
}
