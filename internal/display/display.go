// Package display formats resolved values for human-facing output. A long hash -
// a git commit SHA or a sha256 sum - is abbreviated to a readable head…tail form
// so it does not dominate a log line, while versions and other short values pass
// through unchanged. The full value is always what clover writes to a file; this
// governs only what is shown.
package display

import xstrings "github.com/gechr/x/strings"

// width is the length an abbreviated hash is shortened to, including the marker;
// it keeps enough of both ends to stay recognizable against the full value.
const width = 13

// marker joins the head and tail of an abbreviated value.
const marker = "…"

// Value returns v formatted for display: a commit or sha256 hash is abbreviated
// to a head…tail form; any other value is returned unchanged.
func Value(v string) string {
	if !IsHash(v) {
		return v
	}
	return xstrings.TruncateMiddle(v, width, marker)
}

// IsHash reports whether v is a git commit SHA or a sha256 sum: exactly 40 or 64
// hexadecimal characters.
func IsHash(v string) bool {
	return xstrings.IsGitCommit(v) || xstrings.IsSHA256(v)
}
