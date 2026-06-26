package directive

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
)

// IgnoreScope is the reach of a clover:ignore control comment.
type IgnoreScope int

const (
	// IgnoreNone means the comment is not an ignore control (an ordinary
	// directive, or no directive at all).
	IgnoreNone IgnoreScope = iota
	// IgnoreNextLine ignores a directive on the line immediately following.
	IgnoreNextLine
	// IgnoreBlockStart begins a region ignored until IgnoreBlockEnd.
	IgnoreBlockStart
	// IgnoreBlockEnd ends a region begun by IgnoreBlockStart.
	IgnoreBlockEnd
	// IgnoreFile ignores every directive in the file.
	IgnoreFile
)

// ParseIgnore classifies a comment body as a clover:ignore control, returning
// IgnoreNone when it is an ordinary directive. Only the first token is matched,
// so a trailing explanation is allowed (clover:ignore-file why...), while an
// ordinary clover: directive or a real key like clover:ignored=true is left
// alone.
func ParseIgnore(body string) IgnoreScope {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return IgnoreNone
	}
	switch fields[0] {
	case constant.IgnoreFile:
		return IgnoreFile
	case constant.IgnoreStart:
		return IgnoreBlockStart
	case constant.IgnoreEnd:
		return IgnoreBlockEnd
	case constant.Ignore:
		return IgnoreNextLine
	default:
		return IgnoreNone
	}
}
