package pattern

import (
	"fmt"
	"regexp"

	"github.com/gobwas/glob"
)

// delim wraps a regex pattern: a value bracketed by it is a regex, anything
// else is a glob. Chosen for sed/perl familiarity over a re: prefix.
const delim = '/'

// minRegexLen is the shortest regex value: the two delimiters around an empty
// pattern (//).
const minRegexLen = 2

// Kind distinguishes the two pattern dialects.
type Kind int

const (
	KindGlob  Kind = iota // whole-string glob; a metacharacter-free glob is its own literal
	KindRegex             // RE2, unanchored (substring); bound with ^ and $
)

// Pattern is a compiled match expression with a single [Pattern.Matches]
// predicate, reused by include/exclude today and rewriter conditions and find
// later. The two kinds collapse a would-be "literal" kind: a bare glob with no
// metacharacters already matches exactly, and a backslash escapes the rest.
//
// The raw source is retained so a pattern round-trips back to the text the user
// wrote - the form format mode reproduces and config persists.
type Pattern struct {
	kind Kind
	raw  string
	glob glob.Glob
	re   *regexp.Regexp
}

// Compile parses a pattern value. A value bracketed by / is compiled as an
// unanchored RE2 regex; every other value is a whole-string glob. Glob uses no
// separators, so * spans every character (including /) - a tag filter matches
// opaque tokens, not filesystem paths.
func Compile(raw string) (*Pattern, error) {
	if isRegex(raw) {
		expr := raw[1 : len(raw)-1]
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("compile regex pattern %q: %w", raw, err)
		}
		return &Pattern{kind: KindRegex, raw: raw, re: re}, nil
	}

	g, err := glob.Compile(raw)
	if err != nil {
		return nil, fmt.Errorf("compile glob pattern %q: %w", raw, err)
	}
	return &Pattern{kind: KindGlob, raw: raw, glob: g}, nil
}

// Matches reports whether s satisfies the pattern: a whole-string glob match,
// or an unanchored regex search.
func (p *Pattern) Matches(s string) bool {
	switch p.kind {
	case KindGlob:
		return p.glob.Match(s)
	case KindRegex:
		return p.re.MatchString(s)
	}
	return false
}

// Kind reports which dialect the pattern was compiled as.
func (p *Pattern) Kind() Kind { return p.kind }

// String returns the raw pattern text as written by the user.
func (p *Pattern) String() string { return p.raw }

// isRegex reports whether raw is bracketed by the regex delimiter. A lone / is
// a glob matching a literal slash, not an empty regex.
func isRegex(raw string) bool {
	return len(raw) >= minRegexLen && raw[0] == delim && raw[len(raw)-1] == delim
}
