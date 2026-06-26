package directive

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/constant"
)

// Parse reads a comment body, scans for the clover: keyword, and parses the
// directive that follows. found is false when the body carries no keyword (the
// common case for most lines), in which case there is no error. A keyword
// followed by malformed text - an unterminated quoted or /regex/ value, or an
// empty key - yields found=true and an error so callers can surface it.
//
// The keyword must lead the comment (after optional whitespace), so an
// incidental "clover:" inside prose is not mistaken for a directive.
func Parse(body string) (Directive, bool, error) {
	trimmed := strings.TrimLeft(body, " \t")
	if !strings.HasPrefix(trimmed, constant.DirectiveKeyword) {
		return Directive{}, false, nil
	}

	pairs, err := parsePairs([]rune(trimmed[len(constant.DirectiveKeyword):]))
	if err != nil {
		return Directive{}, true, fmt.Errorf("parse directive: %w", err)
	}
	return Directive{Pairs: pairs}, true, nil
}

// parsePairs splits a directive body into key/value pairs. Pairs are separated
// by whitespace and every key must have a value: a bare key (no =) is an error,
// so booleans are written explicitly as key=true / key=false.
func parsePairs(rs []rune) ([]KV, error) {
	var pairs []KV
	pos, end := 0, len(rs)

	for pos < end {
		for pos < end && isSpace(rs[pos]) {
			pos++
		}
		if pos >= end {
			break
		}

		start := pos
		for pos < end && rs[pos] != constant.DirectiveEqual && !isSpace(rs[pos]) {
			pos++
		}
		key := string(rs[start:pos])
		if key == "" {
			return nil, errors.New("empty key")
		}
		if pos >= end || rs[pos] != constant.DirectiveEqual {
			return nil, fmt.Errorf("key %q must have a value", key)
		}

		pos++ // consume '='
		value, next, err := readValue(rs, pos)
		if err != nil {
			return nil, err
		}
		pos = next
		pairs = append(pairs, KV{Key: key, Value: value})
	}

	return pairs, nil
}

// readValue reads a value beginning at pos and returns it with the position
// just past it. The first character decides the form: a quote (' " `) starts a
// matched span whose quotes are stripped; a leading / starts a regex whose
// delimiters are kept (the pattern package needs them); anything else is a bare
// value read to the next whitespace. Delimiting is recognised only at the start
// of a value, so / and quotes inside a bare value (owner/repo, don't) stay
// literal.
func readValue(rs []rune, pos int) (string, int, error) {
	switch {
	case pos < len(rs) && isQuote(rs[pos]):
		return readQuoted(rs, pos)
	case pos < len(rs) && rs[pos] == '/':
		return readRegex(rs, pos)
	default:
		start := pos
		for pos < len(rs) && !isSpace(rs[pos]) {
			pos++
		}
		return string(rs[start:pos]), pos, nil
	}
}

// readQuoted reads a span opened by the quote at pos, stripping the quotes. The
// matching quote closes it; the other quote characters are literal inside.
func readQuoted(rs []rune, pos int) (string, int, error) {
	quote := rs[pos]
	pos++

	var b strings.Builder
	for pos < len(rs) && rs[pos] != quote {
		b.WriteRune(rs[pos])
		pos++
	}
	if pos >= len(rs) {
		return "", pos, errors.New("unterminated quote")
	}
	return b.String(), pos + 1, nil
}

// readRegex reads a /regex/ value opened at pos, keeping the delimiters. A
// backslash escapes the following character, so an escaped slash does not close
// the span.
func readRegex(rs []rune, pos int) (string, int, error) {
	var b strings.Builder
	b.WriteRune(rs[pos])
	pos++

	for pos < len(rs) && rs[pos] != '/' {
		if rs[pos] == '\\' && pos+1 < len(rs) {
			b.WriteRune(rs[pos])
			pos++
		}
		b.WriteRune(rs[pos])
		pos++
	}
	if pos >= len(rs) {
		return "", pos, errors.New("unterminated regex")
	}
	b.WriteRune(rs[pos])
	return b.String(), pos + 1, nil
}

// isQuote reports whether r is one of the recognised quote characters.
func isQuote(r rune) bool {
	return r == '\'' || r == '"' || r == '`'
}

// isSpace reports whether r separates directive tokens.
func isSpace(r rune) bool {
	return r == ' ' || r == '\t'
}
