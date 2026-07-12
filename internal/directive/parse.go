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
// The @clover form is shorthand for auto mode: a bare @clover parses as
// provider=auto, and pairs require a colon (@clover: key=value). An explicit
// provider pair is redundant but unambiguous, so it simply wins over the
// implied auto (format then canonicalizes the spelling).
//
// The keyword must lead the comment (after optional whitespace), so an
// incidental "clover:" inside prose is not mistaken for a directive. A keyword
// whose colon is missing or detached ahead of pair-shaped text (clover foo=bar,
// @clover : constraint=minor) is reported as malformed - see [malformedColon].
func Parse(body string) (Directive, bool, error) {
	trimmed := strings.TrimLeft(body, " \t")
	rest, auto := cutAutoKeyword(trimmed)
	if !auto {
		var ok bool
		rest, ok = strings.CutPrefix(trimmed, constant.DirectiveKeyword)
		if !ok {
			if kw, malformed := malformedColon(trimmed); malformed {
				return Directive{}, true, fmt.Errorf(
					"parse directive: expected %q immediately after %q",
					string(constant.DirectiveColon),
					kw,
				)
			}
			return Directive{}, false, nil
		}
	}

	pairs, err := parsePairs([]rune(rest))
	if err != nil {
		return Directive{}, true, fmt.Errorf("parse directive: %w", err)
	}
	d := Directive{Pairs: pairs}
	if auto && !d.Has(constant.DirectiveProvider) {
		d.Pairs = append(
			[]KV{{Key: constant.DirectiveProvider, Value: constant.ProviderAuto}},
			d.Pairs...,
		)
	}
	return d, true, nil
}

// malformedColon reports whether trimmed opens with a directive keyword whose
// colon is missing or detached: the bare keyword followed by whitespace and a
// first token that carries an = (clover foo=bar) or leads with a colon
// (@clover : foo=bar). Either token betrays directive intent, so the typo
// surfaces as a malformed directive, while prose that merely leads with the
// word (clover run updates this, @clover please review) stays inert.
func malformedColon(trimmed string) (string, bool) {
	for _, kw := range []string{constant.DirectiveAutoKeyword, constant.DirectiveStem} {
		rest, ok := strings.CutPrefix(trimmed, kw)
		if !ok || rest == "" || !isSpace(rune(rest[0])) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if strings.ContainsRune(fields[0], constant.DirectiveEqual) ||
			fields[0][0] == byte(constant.DirectiveColon) {
			return kw, true
		}
	}
	return "", false
}

// cutAutoKeyword strips a leading @clover keyword, returning what follows. The
// bare form must be the whole body (trailing whitespace aside) and pairs must
// open with a colon, so prose like @cloverfield or an "@clover please review"
// mention is not mistaken for the shorthand. The colon is consumed; what
// remains parses as usual.
func cutAutoKeyword(trimmed string) (string, bool) {
	rest, ok := strings.CutPrefix(trimmed, constant.DirectiveAutoKeyword)
	if !ok {
		return "", false
	}
	switch {
	case strings.TrimLeft(rest, " \t") == "":
		return "", true
	case rest[0] == byte(constant.DirectiveColon):
		return rest[1:], true
	default:
		return "", false // prose, e.g. @cloverfield or @clover the handle
	}
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
