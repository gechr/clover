package match

import (
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/version"
	xstrings "github.com/gechr/x/strings"
)

// maxComponents is the most numeric components a version core may have. Four or
// more (1.2.3.4) is rejected as not version-shaped.
const maxComponents = 3

// Span is a byte range [Start, End) within a line.
type Span struct {
	Start int
	End   int
}

// Token is a version reference located in a line: its byte span plus the parts
// it decomposes into. Keeping the numeric Core separate from the Prefix,
// Prerelease, variant Suffix, and Build means the rewriter can normalise on the
// core and re-apply each decoration exactly once when rendering a new version.
type Token struct {
	Span       Span
	Prefix     string // "v", or "" when bare
	Core       string // numeric core, 1-3 dotted components, e.g. "1.27"
	Prerelease string // prerelease identifiers without the leading -, or ""
	Dashless   bool   // prerelease glued to the core with no dash (3.15.0b3)
	Suffix     string // recognised variant suffix without the leading -, or ""
	Build      string // build metadata without the leading +, or ""
}

// Find returns every version-shaped token in line, in order. It is the whole-
// line scan the smart rewriter uses where a provider offers no anchor; matching
// is deliberately strict (see scanToken) to keep false positives rare.
func Find(line string) []Token {
	var tokens []Token
	for i := 0; i < len(line); {
		if canStart(line, i) {
			if tok, end, ok := scanToken(line, i); ok {
				tokens = append(tokens, tok)
				i = end
				continue
			}
		}
		i++
	}
	return tokens
}

// Shaped reports whether s carries a version-shaped token, i.e. [Find] locates
// one. A restyling rewriter re-dresses a resolved version onto the current
// token's shape, and a value with no version-shaped token (a leading-zero
// component like 19.0614, a four-component core) has no shape to re-dress -
// rendering it would fabricate a version, so such a candidate must not be
// selected.
func Shaped(s string) bool { return len(Find(s)) > 0 }

// canStart reports whether a token may begin at i: the previous byte must not
// continue a token (an alphanumeric or dot, which would mean we are mid-word or
// mid-number), and i must begin a v-prefix or a digit.
func canStart(line string, i int) bool {
	if i > 0 && (xstrings.IsAlphanumericChar(rune(line[i-1])) || line[i-1] == '.') {
		return false
	}
	if line[i] == 'v' {
		return i+1 < len(line) && xstrings.IsDigitChar(rune(line[i+1]))
	}
	return xstrings.IsDigitChar(rune(line[i]))
}

// scanToken attempts to read a whole version token at start, returning it with
// the byte position just past it. It rejects anything that is not cleanly
// version-shaped: a four-component core, a leading-zero component (calver like
// 2024.01.15), or a core that runs straight into letters (java25).
func scanToken(line string, start int) (Token, int, bool) {
	i := start

	var prefix string
	if line[i] == 'v' {
		prefix, i = "v", i+1
	}

	core, next, ok := scanCore(line, i)
	if !ok {
		return Token{}, start, false
	}
	i = next

	// A following .digit means a fourth component: not version-shaped.
	if i+1 < len(line) && line[i] == '.' && xstrings.IsDigitChar(rune(line[i+1])) {
		return Token{}, start, false
	}

	var prerelease, suffix string
	var dashless bool
	if seg, end, ok := scanDashedSegment(line, i); ok {
		if version.IsVariant(seg) {
			suffix = seg
		} else {
			prerelease = seg
		}
		i = end
	} else if seg, end, ok := scanDashlessPre(line, i); ok {
		prerelease, dashless, i = seg, true, end
	}

	var build string
	if i < len(line) && line[i] == '+' {
		if seg, end := scanSegment(line, i+1); seg != "" {
			build, i = seg, end
		}
	}

	// The token must end at a boundary; a trailing letter or digit means it ran
	// into something that is not part of a version (1.2abc).
	if i < len(line) && xstrings.IsAlphanumericChar(rune(line[i])) {
		return Token{}, start, false
	}

	return Token{
		Span:       Span{Start: start, End: i},
		Prefix:     prefix,
		Core:       core,
		Prerelease: prerelease,
		Dashless:   dashless,
		Suffix:     suffix,
		Build:      build,
	}, i, true
}

// scanDashedSegment reads the -prefixed prerelease-or-suffix segment after the
// core, when one is present.
func scanDashedSegment(line string, start int) (string, int, bool) {
	if start >= len(line) || line[start] != constant.VersionDash {
		return "", start, false
	}
	seg, end := scanSegment(line, start+1)
	if seg == "" {
		return "", start, false
	}
	return seg, end, true
}

// scanDashlessPre reads a PEP 440-style prerelease glued straight onto the core
// (3.15.0b3, 3.14.0rc1): one of a, b, c, or rc, then digits. Only that exact
// grammar matches, so an arbitrary letter run (1.2abc) stays rejected by the
// caller's boundary check.
func scanDashlessPre(line string, start int) (string, int, bool) {
	i := start
	for i < len(line) && line[i] >= 'a' && line[i] <= 'z' {
		i++
	}
	switch tag := line[start:i]; tag {
	case "a", "b", "c", "rc":
	default:
		return "", start, false
	}
	j := i
	for j < len(line) && xstrings.IsDigitChar(rune(line[j])) {
		j++
	}
	if j == i {
		return "", start, false
	}
	return line[start:j], j, true
}

// scanCore reads 1 to maxComponents dot-separated numeric components and returns
// the core string. A multi-digit component with a leading zero is rejected.
func scanCore(line string, start int) (string, int, bool) {
	i, components := start, 0
	for components < maxComponents {
		j := i
		for j < len(line) && xstrings.IsDigitChar(rune(line[j])) {
			j++
		}
		if j == i {
			break
		}
		if j-i > 1 && line[i] == '0' {
			return "", start, false
		}
		i, components = j, components+1
		if i < len(line) && line[i] == '.' && i+1 < len(line) &&
			xstrings.IsDigitChar(rune(line[i+1])) {
			i++ // consume the dot and read the next component
			continue
		}
		break
	}
	if components == 0 {
		return "", start, false
	}
	return line[start:i], i, true
}

// scanSegment reads a prerelease, suffix, or build run up to the next boundary.
func scanSegment(line string, start int) (string, int) {
	i := start
	for i < len(line) && isSegmentByte(line[i]) {
		i++
	}
	return line[start:i], i
}

func isSegmentByte(b byte) bool {
	return xstrings.IsAlphanumericChar(rune(b)) || b == '.' || b == constant.VersionDash
}
