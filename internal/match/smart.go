package match

import (
	"errors"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
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

// Locate finds the single version token on line. It errors when the line has no
// version-shaped token, or more than one (the ambiguity the design fails loud on
// rather than guessing). Locate does no I/O, so lint runs it to validate a
// marker without resolving anything.
func (Smart) Locate(line string) (Located, error) {
	tokens := Find(line)
	switch len(tokens) {
	case 0:
		return nil, errors.New("no version found on target line")
	case 1:
		return locatedToken(line, tokens[0]), nil
	default:
		return nil, errors.New("multiple version-shaped tokens; target is ambiguous")
	}
}

// smartLocated is a single version token the smart (and docker-tag) rewriter
// re-styles in place. The token carries the span and the original's style.
type smartLocated struct {
	anchored

	token Token
}

// locatedToken builds a smartLocated for a token already offset into line.
func locatedToken(line string, token Token) smartLocated {
	semver, _ := version.Parse(token.Core) // nil core only matters to a keyword constraint
	return smartLocated{
		anchored: anchored{raw: line[token.Span.Start:token.Span.End], semver: semver},
		token:    token,
	}
}

// Rendered reports the version text Render will write for candidate - the token
// restyled onto the current's shape, which may differ from candidate.Version (a
// stripped variant, a re-precisioned or re-prefixed core). The report shows this
// as the new value, so it matches what lands in the file.
func (l smartLocated) Rendered(candidate model.Candidate) string {
	return restyle(l.token, candidate.Version)
}

// Render rewrites the located token on line to the resolved candidate's version,
// returning the new line and whether it changed. The version is normalised to
// its numeric core and re-dressed in the current token's style, so the splice
// touches only the token's span and is idempotent. It errors when the located
// span no longer fits the line.
func (l smartLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	span := l.token.Span
	if span.Start < 0 || span.End > len(line) || span.Start > span.End {
		return "", false, errors.New("located version span no longer fits the line")
	}

	rendered := restyle(l.token, candidate.Version)
	old := line[span.Start:span.End]
	if rendered == old {
		return line, false, nil
	}
	return line[:span.Start] + rendered + line[span.End:], true, nil
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
