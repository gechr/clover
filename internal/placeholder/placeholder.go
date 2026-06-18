// Package placeholder is the bidirectional <token> system shared by find/replace:
// the same token (e.g. <major.minor>) matches a shape inside a glob find pattern
// and renders a value inside a replace template. It is a pure string utility - it
// knows the token spellings and their match-regexes, not how a value is computed.
//
// Angle brackets avoid the ambiguity braces would carry: {n} is a regex
// quantifier and {a,b} a glob alternation, so <token> is unmistakable in both.
package placeholder

import (
	"fmt"
	"regexp"
	"strings"
)

// matchRegex maps a token to the regex it matches inside a glob find pattern.
// hex is match-only (no rendered value); the rest can also render.
var matchRegex = map[string]string{
	"version":           `v?\d+(?:\.\d+){0,2}(?:-[0-9A-Za-z.]+)?`,
	"major":             `\d+`,
	"minor":             `\d+`,
	"patch":             `\d+`,
	"major.minor":       `\d+\.\d+`,
	"major.minor.patch": `\d+\.\d+\.\d+`,
	"commit":            `[0-9a-fA-F]{40}`,
	"sha256":            `[0-9a-fA-F]{64}`,
	"hex":               `[0-9a-fA-F]+`,
}

// token matches a <placeholder>: dot-separated alphanumeric segments, so a dot
// is only ever a separator (never leading, trailing, or doubled).
var token = regexp.MustCompile(`<[a-z0-9]+(?:\.[a-z0-9]+)*>`)

// Compile turns a glob find pattern into an unanchored regex plus the token
// names it captures, one per capture group in order. A <token> (matched the same
// way as Expand, so a tight inner pair wins and stray angle brackets are literal)
// becomes a capturing group of its shape; * matches any run, ? any char, and
// everything else is literal. An unknown <token> is an error.
func Compile(glob string) (*regexp.Regexp, []string, error) {
	var (
		b      strings.Builder
		tokens []string
	)
	last := 0
	for _, loc := range token.FindAllStringIndex(glob, -1) {
		writeGlob(&b, glob[last:loc[0]])
		name := glob[loc[0]+1 : loc[1]-1]
		re, ok := matchRegex[name]
		if !ok {
			return nil, nil, fmt.Errorf("placeholder: unknown token <%s>", name)
		}
		b.WriteString("(" + re + ")")
		tokens = append(tokens, name)
		last = loc[1]
	}
	writeGlob(&b, glob[last:])

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, nil, fmt.Errorf("placeholder: compile %q: %w", glob, err)
	}
	return re, tokens, nil
}

// writeGlob writes a non-token segment as regex: * matches any run, ? any char,
// everything else is a literal.
func writeGlob(b *strings.Builder, s string) {
	for i := range len(s) {
		switch s[i] {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(s[i : i+1]))
		}
	}
}

// Expand fills a replace template, substituting each provided token for its
// value and leaving the rest untouched.
func Expand(template string, values map[string]string) string {
	return token.ReplaceAllStringFunc(template, func(t string) string {
		if v, ok := values[t[1:len(t)-1]]; ok {
			return v
		}
		return t
	})
}

// HasToken reports whether s still contains a <token>, so a caller can reject a
// replace that referenced an unsupported or unavailable one.
func HasToken(s string) bool { return token.MatchString(s) }
