package match

import (
	"strings"

	"github.com/gechr/cusp/internal/constant"
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
	Suffix     string // recognised variant suffix without the leading -, or ""
	Build      string // build metadata without the leading +, or ""
}

// variants are the recognised image variant suffixes - distro flavours and
// codenames that decorate a tag (nginx:1.27-alpine) and must be preserved, as
// opposed to a semver prerelease (2.0.0-rc.1) which may be trimmed. The set is
// curated; unknown trailing segments are treated as prereleases.
var variants = map[string]bool{
	"alpine": true,
	"slim":   true,
	// Debian release codenames.
	"buster":   true,
	"bullseye": true,
	"bookworm": true,
	"trixie":   true,
	"sid":      true,
	// Ubuntu release codenames.
	"bionic": true,
	"focal":  true,
	"jammy":  true,
	"noble":  true,
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

// canStart reports whether a token may begin at i: the previous byte must not
// continue a token (an alphanumeric or dot, which would mean we are mid-word or
// mid-number), and i must begin a v-prefix or a digit.
func canStart(line string, i int) bool {
	if i > 0 && (isAlnum(line[i-1]) || line[i-1] == '.') {
		return false
	}
	if line[i] == 'v' {
		return i+1 < len(line) && isDigit(line[i+1])
	}
	return isDigit(line[i])
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
	if i+1 < len(line) && line[i] == '.' && isDigit(line[i+1]) {
		return Token{}, start, false
	}

	var prerelease, suffix string
	if i < len(line) && line[i] == constant.VersionDash {
		if seg, end := scanSegment(line, i+1); seg != "" {
			if isVariant(seg) {
				suffix = seg
			} else {
				prerelease = seg
			}
			i = end
		}
	}

	var build string
	if i < len(line) && line[i] == '+' {
		if seg, end := scanSegment(line, i+1); seg != "" {
			build, i = seg, end
		}
	}

	// The token must end at a boundary; a trailing letter or digit means it ran
	// into something that is not part of a version (1.2abc).
	if i < len(line) && isAlnum(line[i]) {
		return Token{}, start, false
	}

	return Token{
		Span:       Span{Start: start, End: i},
		Prefix:     prefix,
		Core:       core,
		Prerelease: prerelease,
		Suffix:     suffix,
		Build:      build,
	}, i, true
}

// scanCore reads 1 to maxComponents dot-separated numeric components and returns
// the core string. A multi-digit component with a leading zero is rejected.
func scanCore(line string, start int) (string, int, bool) {
	i, components := start, 0
	for components < maxComponents {
		j := i
		for j < len(line) && isDigit(line[j]) {
			j++
		}
		if j == i {
			break
		}
		if j-i > 1 && line[i] == '0' {
			return "", start, false
		}
		i, components = j, components+1
		if i < len(line) && line[i] == '.' && i+1 < len(line) && isDigit(line[i+1]) {
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

// isVariant reports whether a trailing segment is a recognised image variant
// (so a suffix to preserve) rather than a prerelease. It matches on the first
// dash-delimited word with any trailing version digits stripped, so alpine3.19
// and slim-bookworm both register as variants.
func isVariant(segment string) bool {
	word, _, _ := strings.Cut(segment, string(constant.VersionDash))
	word = strings.TrimRight(word, "0123456789.")
	return variants[strings.ToLower(word)]
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isAlnum(b byte) bool {
	return isDigit(b) || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isSegmentByte(b byte) bool { return isAlnum(b) || b == '.' || b == constant.VersionDash }
