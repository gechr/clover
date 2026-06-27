package pattern

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gechr/clover/internal/regexlit"
	"github.com/gobwas/glob"
)

// Kind distinguishes the two pattern dialects.
type Kind int

const (
	KindGlob  Kind = iota // whole-string glob; a metacharacter-free glob is its own literal
	KindRegex             // RE2, unanchored (substring); bound with ^ and $
)

// Token is the name of a <placeholder> the grammar recognizes, e.g. <version>.
type Token string

// Token names the grammar recognizes - the keys of matchRegex, the names a find
// captures, and the keys Expand substitutes. They are the single source of truth
// for the vocabulary, shared by the match and checksum packages so a token name
// is never spelled as a literal.
const (
	TokenCommit          Token = "commit"
	TokenHex             Token = "hex"
	TokenMajor           Token = "major"
	TokenMajorMinor      Token = "major.minor"
	TokenMajorMinorPatch Token = "major.minor.patch"
	TokenMinor           Token = "minor"
	TokenPatch           Token = "patch"
	TokenSHA256          Token = "sha256"
	TokenVersion         Token = "version"
)

// Tokens is an ordered list of token names, as a glob find captures them.
type Tokens []Token

// TokenMap maps a token to a substitution value, the input to Expand.
type TokenMap map[Token]string

// matchRegex maps a token to the regex it matches inside a glob. hex is
// match-only (no rendered value); the rest can also render.
//
//nolint:gosec // G101 false positive: a token name, not a hardcoded credential.
var matchRegex = TokenMap{
	TokenCommit:          `[0-9a-fA-F]{40}`,
	TokenHex:             `[0-9a-fA-F]+`,
	TokenMajor:           `\d+`,
	TokenMajorMinor:      `\d+\.\d+`,
	TokenMajorMinorPatch: `\d+\.\d+\.\d+`,
	TokenMinor:           `\d+`,
	TokenPatch:           `\d+`,
	TokenSHA256:          `[0-9a-fA-F]{64}`,
	TokenVersion:         `v?\d+(?:\.\d+){0,2}(?:-[0-9A-Za-z.]+)?(?:\+[0-9A-Za-z.-]+)?`,
}

// token matches a <placeholder>: dot-separated alphanumeric segments, so a dot
// is only ever a separator (never leading, trailing, or doubled).
var token = regexp.MustCompile(`<[a-z0-9]+(?:\.[a-z0-9]+)*>`)

// Pattern is a compiled match expression. The two kinds collapse a would-be
// "literal" kind: a bare glob with no metacharacters already matches exactly,
// and a backslash escapes the rest. The raw source is retained so a pattern
// round-trips back to the text the user wrote.
type Pattern struct {
	kind   Kind
	raw    string
	glob   glob.Glob      // whole-string matcher (KindGlob)
	re     *regexp.Regexp // unanchored: /regex/ (KindRegex), else the glob's capture regex
	tokens Tokens         // capture-group token names in order; nil for KindRegex
}

// Compile parses a pattern value. A value bracketed by / is compiled as an
// unanchored RE2 regex; every other value is a whole-string glob that may carry
// <placeholders>. Glob uses no separators, so * spans every character (including
// /) - a tag filter matches opaque tokens, not filesystem paths.
func Compile(raw string) (*Pattern, error) {
	if expr, ok := regexlit.Body(raw); ok {
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("compile regex pattern %q: %w", raw, err)
		}
		return &Pattern{kind: KindRegex, raw: raw, re: re}, nil
	}

	re, tokens, err := compileGlob(raw)
	if err != nil {
		return nil, err
	}
	// The whole-string matcher reads each <token> as a run of any character, so a
	// token-bearing glob still filters (app-test/<version> matches app-test/v1).
	g, err := glob.Compile(token.ReplaceAllString(raw, "*"))
	if err != nil {
		return nil, fmt.Errorf("compile glob pattern %q: %w", raw, err)
	}
	return &Pattern{kind: KindGlob, raw: raw, glob: g, re: re, tokens: tokens}, nil
}

// compileGlob turns a glob into an unanchored capture regex plus the token names
// it captures, one per group in order. A <token> becomes a capturing group of
// its shape; * matches any run, ? any char, everything else is literal. An
// unknown <token> is an error.
func compileGlob(raw string) (*regexp.Regexp, Tokens, error) {
	var (
		b      strings.Builder
		tokens Tokens
	)
	last := 0
	for _, loc := range token.FindAllStringIndex(raw, -1) {
		writeGlob(&b, raw[last:loc[0]])
		name := Token(raw[loc[0]+1 : loc[1]-1])
		re, ok := matchRegex[name]
		if !ok {
			return nil, nil, fmt.Errorf("pattern: unknown token <%s>", name)
		}
		b.WriteString("(" + re + ")")
		tokens = append(tokens, name)
		last = loc[1]
	}
	writeGlob(&b, raw[last:])

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, nil, fmt.Errorf("pattern: compile %q: %w", raw, err)
	}
	return re, tokens, nil
}

// writeGlob writes a non-token segment as regex: * matches any run (non-greedy,
// so an adjacent trailing <token> is not starved of characters), ? any char, and
// everything else is a literal.
func writeGlob(b *strings.Builder, s string) {
	for i := range len(s) {
		switch s[i] {
		case '*':
			b.WriteString(".*?")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(s[i : i+1]))
		}
	}
}

// Matches reports whether s satisfies the pattern: a whole-string glob match, or
// an unanchored regex search.
func (p *Pattern) Matches(s string) bool {
	switch p.kind {
	case KindGlob:
		return p.glob.Match(s)
	case KindRegex:
		return p.re.MatchString(s)
	}
	return false
}

// Regexp returns the unanchored regex used to locate and capture: the /regex/
// itself, or the glob's <token>-capturing form.
func (p *Pattern) Regexp() *regexp.Regexp { return p.re }

// Tokens returns the capture-group token names in order, or nil for a /regex/.
func (p *Pattern) Tokens() Tokens { return p.tokens }

// Kind reports which dialect the pattern was compiled as.
func (p *Pattern) Kind() Kind { return p.kind }

// String returns the raw pattern text as written by the user.
func (p *Pattern) String() string { return p.raw }

// Expand fills a replace template, substituting each provided token for its
// value and leaving the rest untouched.
func Expand(template string, values TokenMap) string {
	return token.ReplaceAllStringFunc(template, func(t string) string {
		if v, ok := values[Token(t[1:len(t)-1])]; ok {
			return v
		}
		return t
	})
}

// HasToken reports whether s still contains a <token>, so a caller can reject a
// replace that referenced an unsupported or unavailable one.
func HasToken(s string) bool { return token.MatchString(s) }
