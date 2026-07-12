package sidecar

import (
	"encoding/json"
	"strconv"
	"strings"

	xstrings "github.com/gechr/x/strings"
)

// parsePath parses a literal jq path expression - the bracket and dot forms
// pathToJQ emits and hand-written locators overwhelmingly use (.a.b,
// .["k"][0], .versions[-1]) - into path segments. ok is false for anything
// beyond a literal path (a pipe, a wildcard, a slice, a function call), which
// gojq derivation owns.
func parsePath(expr string) ([]any, bool) {
	rest, found := strings.CutPrefix(expr, ".")
	if !found {
		return nil, false
	}
	path := []any{}
	// An identifier may follow the root dot directly (.a); every later object
	// segment carries its own dot, so a bare second dot (..a, jq's recursive
	// descent) never parses as a path.
	if n := identLen(rest); n > 0 {
		path = append(path, rest[:n])
		rest = rest[n:]
	}
	for rest != "" {
		if next, ok := strings.CutPrefix(rest, "."); ok && len(path) > 0 {
			n := identLen(next)
			if n == 0 {
				return nil, false
			}
			path = append(path, next[:n])
			rest = next[n:]
			continue
		}
		next, ok := strings.CutPrefix(rest, "[")
		if !ok {
			return nil, false
		}
		seg, after, ok := parseBracket(next)
		if !ok {
			return nil, false
		}
		path = append(path, seg)
		rest = after
	}
	return path, true
}

// parseBracket parses one bracket segment's body - a JSON string key or an
// integer index - returning the segment and the remainder past the closing
// bracket.
func parseBracket(s string) (any, string, bool) {
	if strings.HasPrefix(s, `"`) {
		end := jsonStringEnd(s)
		if end < 0 || !strings.HasPrefix(s[end:], "]") {
			return nil, "", false
		}
		var key string
		if err := json.Unmarshal([]byte(s[:end]), &key); err != nil {
			return nil, "", false
		}
		return key, s[end+1:], true
	}

	before, after, ok := strings.Cut(s, "]")
	if !ok {
		return nil, "", false
	}
	digits := strings.TrimPrefix(before, "-")
	// Match jq's number lexing: bare digits only, no leading zeros (a lone 0
	// aside), so an expression jq itself would reject falls through to gojq's
	// error.
	if !xstrings.IsDigits(digits) || (len(digits) > 1 && digits[0] == '0') {
		return nil, "", false
	}
	idx, err := strconv.Atoi(before)
	if err != nil {
		return nil, "", false
	}
	return idx, after, true
}

// jsonStringEnd returns the index just past the closing quote of the JSON
// string literal s starts with, or -1 when it never closes.
func jsonStringEnd(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return -1
}

// identLen returns the length of the jq identifier prefixing s: an ASCII
// letter or underscore, then letters, digits, or underscores.
func identLen(s string) int {
	for i := range len(s) {
		b := s[i]
		switch {
		case b == '_' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z':
		case '0' <= b && b <= '9' && i > 0:
		default:
			return i
		}
	}
	return len(s)
}
