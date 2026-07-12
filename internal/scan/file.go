package scan

import (
	"bytes"
	"io"
	"os"
	"strings"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/comment"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/x/set"
	xstrings "github.com/gechr/x/strings"
)

const prefilterChunkSize = 32 << 10

// directiveStemBytes is the prefilter needle: every directive form (clover:,
// @clover, the clover:ignore controls) contains the bare stem.
var directiveStemBytes = []byte(constant.DirectiveStem)

// Located is a directive found on a line of a file. An inline directive's Line
// is its comment line (it rewrites the line below); a sidecar directive's Line
// is the target line itself, already resolved by its locator, so Sidecar marks
// which binding the pipeline applies.
type Located struct {
	Line      int // index into File.Lines of the directive's comment line, or the target line for a sidecar
	Directive directive.Directive
	Sidecar   bool // the directive came from a YAML sidecar; Line is the resolved target
}

// LineError is a malformed directive: the keyword was present but parsing
// failed. The pipeline surfaces it as an errored marker result. Sidecar marks a
// structural sidecar problem (a dangling target, an unresolvable locator,
// double-governance) so lint fails on it while run downgrades it to a
// skip-with-warning - a broken sidecar should not sink an otherwise-good run.
// Skip marks a soft skip rather than a hard error - a sidecar entry suppressed
// by a clover:ignore on its target line - surfaced as a skipped result (its Err
// carries the reason), so the opt-out is visible without reading as a failure.
type LineError struct {
	Line    int
	Err     error
	Sidecar bool
	Skip    bool
}

// File is a scanned file that carries at least one directive. Lines is the
// file's content, split on newlines and retained so the apply phase can rewrite
// in place.
type File struct {
	Path  string
	Lines []string
	Found []Located
	// Ignored holds the line indices a clover:ignore control suppresses (the
	// next-line target, or the lines inside an ignore-start/ignore-end block).
	// It lets annotate honor the same opt-out the directive scan does; it is nil
	// when the file has no such control.
	Ignored set.Set[int]
	Errors  []LineError
}

// scanFile reads path and extracts its directives. ok is false when the file is
// missing, too large, or binary; when requireDirective is set it is also false
// for a file carrying no directive at all. With requireDirective off the file is
// returned even when it has no directive (Found is empty), which is what annotate
// needs to inspect every line of an as-yet-unannotated file.
func scanFile(path string, size, maxSize int64, requireDirective bool) (File, bool) {
	if size < 0 {
		info, err := os.Stat(path)
		if err != nil {
			skipFile(path, "stat failed").Msg("Skipping file")
			return File{}, false
		}
		if !info.Mode().IsRegular() {
			skipFile(path, "non-regular").Msg("Skipping file")
			return File{}, false
		}
		size = info.Size()
	}
	if size > maxSize {
		skipFile(
			path,
			"too large",
		).Int64("size", size).
			Int64("max_size", maxSize).
			Msg("Skipping file")
		return File{}, false
	}

	// The prefilter pays off only when most files will be rejected for carrying
	// no directive; with requireDirective off every file is read wholesale
	// anyway, so a prefilter pass would just read each file twice.
	if requireDirective {
		prefilter, reason := maybeTextWithDirective(path)
		if !prefilter {
			skipFile(path, reason).Msg("Skipping file")
			return File{}, false
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		skipFile(path, "read failed").Msg("Skipping file")
		return File{}, false
	}
	if bytes.IndexByte(content, 0) >= 0 {
		skipFile(path, "binary").Msg("Skipping file")
		return File{}, false // binary
	}

	lines := splitLines(content)
	syntax := comment.For(path)

	var (
		found      []Located
		problems   []LineError
		ignored    set.Set[int]
		inBlock    bool
		ignoreLine = -1 // line index suppressed by a preceding clover:ignore
	)
	markIgnored := func(i int) {
		if ignored == nil {
			ignored = set.New[int]()
		}
		ignored.Add(i)
	}
	for i, line := range lines {
		// A line inside an ignore block, or the target of a clover:ignore, is
		// suppressed even when it carries no keyword - recorded here, before the
		// keyword prefilter, so annotate skips it just as the directive scan does.
		if inBlock || i == ignoreLine {
			markIgnored(i)
		}
		if !strings.Contains(line, constant.DirectiveStem) {
			continue // cheap prefilter: most lines have no keyword
		}
		body, ok := syntax.Body(line)
		if !ok {
			continue
		}

		switch directive.ParseIgnore(body) {
		case directive.IgnoreFile:
			skipFile(path, "clover ignore file").Msg("Skipping file")
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

	if requireDirective && len(found) == 0 && len(problems) == 0 {
		skipFile(path, "no directive").Msg("Skipping file")
		return File{}, false
	}
	clog.Debug().
		Path(field.Path, path).
		Int(field.Comments, len(found)).
		Int(field.Errors, len(problems)).
		Msg("Found Clover comments")
	return File{Path: path, Lines: lines, Found: found, Ignored: ignored, Errors: problems}, true
}

// maybeTextWithDirective rejects obvious misses before allocating a whole-file
// buffer: binary files with a NUL byte, and files with no clover keyword. It
// returns as soon as the keyword is seen - the whole-file read that follows
// re-checks for a NUL, so the unread remainder cannot smuggle a binary past
// the gate.
func maybeTextWithDirective(path string) (bool, string) {
	file, err := os.Open(path)
	if err != nil {
		return false, "open failed"
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, prefilterChunkSize)
	tail := make([]byte, 0, len(directiveStemBytes)-1)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if bytes.IndexByte(chunk, 0) >= 0 {
				return false, "binary"
			}
			if containsKeyword(tail, chunk) {
				return true, ""
			}
			tail = carry(chunk)
		}
		if err == io.EOF {
			return false, "no clover keyword"
		}
		if err != nil {
			return false, "read failed"
		}
	}
}

func skipFile(path, reason string) *clog.Event {
	return clog.Debug().Path(field.Path, path).Str(field.Reason, reason)
}

// splitLines splits content losslessly so line numbers and content survive for
// the rewrite, normalizing CRLF to LF.
func splitLines(content []byte) []string {
	return xstrings.SplitLinesRaw(string(content))
}

// containsKeyword reports whether chunk contains the directive keyword, either
// wholly inside chunk or split across the previous buffer's tail.
func containsKeyword(tail, chunk []byte) bool {
	if bytes.Contains(chunk, directiveStemBytes) {
		return true
	}
	if len(tail) == 0 {
		return false
	}
	prefixLen := min(len(chunk), len(directiveStemBytes)-1)
	window := make([]byte, 0, len(tail)+prefixLen)
	window = append(window, tail...)
	window = append(window, chunk[:prefixLen]...)
	return bytes.Contains(window, directiveStemBytes)
}

// carry keeps enough trailing bytes to match a directive keyword split across
// two read buffers.
func carry(buf []byte) []byte {
	keep := min(len(buf), len(directiveStemBytes)-1)
	out := make([]byte, keep)
	copy(out, buf[len(buf)-keep:])
	return out
}
