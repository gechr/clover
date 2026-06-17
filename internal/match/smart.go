package match

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
)

// Smart is the default rewriter. It locates a version by shape - no provider
// anchor is needed for the v0.1.0 providers, which are weakly anchored - and
// renders a new version that preserves the original's style: its v-prefix,
// component precision, and variant suffix, trimming a prerelease only when the
// original had none.
type Smart struct{}

// NewSmart returns the default smart rewriter. It takes no arguments today but
// is the seam for future options, and keeps construction uniform with the rest
// of the codebase. The value is returned directly: Smart is stateless, so a
// pointer would only add an allocation and indirection.
func NewSmart() Smart { return Smart{} }

// Locate finds the single version token on line. It reports false when the line
// has no version-shaped token or more than one, which is the ambiguity the
// design fails loud on rather than guessing. Locate does no I/O, so lint can run
// it to validate a marker without resolving anything.
func (Smart) Locate(line string) (Token, bool) {
	tokens := Find(line)
	if len(tokens) != 1 {
		return Token{}, false
	}
	return tokens[0], true
}

// Render rewrites the located token on line to the resolved version, returning
// the new line and whether it changed. The resolved value is normalised to its
// numeric core and then re-dressed in the current token's style, so the splice
// touches only the token's span and is idempotent.
func (Smart) Render(line string, current Token, resolved string) (string, bool) {
	rendered := restyle(current, resolved)
	current.Span.clampTo(line)
	old := line[current.Span.Start:current.Span.End]
	if rendered == old {
		return line, false
	}
	return line[:current.Span.Start] + rendered + line[current.Span.End:], true
}

// restyle produces the new version string: the resolved core at the current's
// precision and prefix, the resolved prerelease only if the current carried
// one, and the current's variant suffix re-applied exactly once.
func restyle(current Token, resolved string) string {
	candidate := decompose(resolved)

	var b strings.Builder
	b.WriteString(current.Prefix)
	b.WriteString(reprecision(candidate.Core, components(current.Core)))
	if current.Prerelease != "" && candidate.Prerelease != "" {
		b.WriteByte(constant.VersionDash)
		b.WriteString(candidate.Prerelease)
	}
	if current.Suffix != "" {
		b.WriteByte(constant.VersionDash)
		b.WriteString(current.Suffix)
	}
	return b.String()
}

// decompose parses a standalone resolved version into its parts. A value that is
// not version-shaped degrades to a bare core so Render still produces output.
func decompose(resolved string) Token {
	if tokens := Find(resolved); len(tokens) > 0 {
		return tokens[0]
	}
	return Token{Core: resolved}
}

// reprecision reformats a numeric core to n components, padding with zeros or
// truncating trailing components as needed.
func reprecision(core string, n int) string {
	parts := strings.Split(core, ".")
	for len(parts) < n {
		parts = append(parts, "0")
	}
	return strings.Join(parts[:n], ".")
}

// components counts the numeric components in a core.
func components(core string) int {
	return strings.Count(core, ".") + 1
}

// clampTo keeps a span within line's bounds, guarding the splice against a span
// located on a different (e.g. since-edited) line.
func (s *Span) clampTo(line string) {
	s.Start = min(max(s.Start, 0), len(line))
	s.End = min(max(s.End, s.Start), len(line))
}
