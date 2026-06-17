// Package regexlit defines clover's /regex/ literal syntax: a value bracketed by
// slashes is a regular expression, anything else is not. It is the single source
// of truth shared by the directive grammar (which parses and renders such
// values) and the pattern engine (which compiles them), so the two can never
// disagree on what counts as a regex.
package regexlit

// Delim opens and closes a regex literal. Slashes are used for sed/perl
// familiarity, chosen over a re: prefix.
const Delim = '/'

// minLen is the shortest literal: the two delimiters around an empty pattern.
const minLen = 2

// Is reports whether s is a complete regex literal - opened and closed by an
// unescaped Delim with no unescaped Delim between. A backslash escapes the next
// character, so /a\/b/ is one literal but /a/b/ is not (and a lone / is a
// plain slash, not an empty regex).
func Is(s string) bool {
	if len(s) < minLen || s[0] != Delim {
		return false
	}
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++ // skip the escaped character
		case Delim:
			return i == len(s)-1 // the closing delimiter must be the final byte
		}
	}
	return false
}

// Body returns the pattern between a literal's delimiters, leaving any internal
// escapes intact for the regex engine. ok is false when s is not a complete
// regex literal.
func Body(s string) (string, bool) {
	if !Is(s) {
		return "", false
	}
	return s[1 : len(s)-1], true
}
