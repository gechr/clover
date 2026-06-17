package ignore

import (
	"regexp"
	"strings"
)

// pattern is one compiled .gitignore line.
type pattern struct {
	negated bool
	dirOnly bool
	re      *regexp.Regexp
}

// match reports whether rel (a slash-separated path relative to the pattern's
// .gitignore directory) is matched. A directory-only pattern matches only a
// directory.
func (p pattern) match(rel string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	return p.re.MatchString(rel)
}

// parse compiles the lines of a .gitignore file into patterns, skipping blanks,
// comments, and any line whose translation fails to compile.
func parse(content string) []pattern {
	var patterns []pattern
	for line := range strings.SplitSeq(content, "\n") {
		if p, ok := compile(line); ok {
			patterns = append(patterns, p)
		}
	}
	return patterns
}

// compile turns one .gitignore line into a pattern. The second result is false
// for blanks, comments, and uncompilable lines. Order matters: strip negation,
// then the trailing slash (directory-only), then determine anchoring.
func compile(line string) (pattern, bool) {
	line = strings.TrimRight(line, " ")
	if line == "" || strings.HasPrefix(line, "#") {
		return pattern{}, false
	}

	var p pattern
	if rest, ok := strings.CutPrefix(line, "!"); ok {
		p.negated, line = true, rest
	}
	if rest, ok := strings.CutSuffix(line, "/"); ok {
		p.dirOnly, line = true, rest
	}

	anchored := strings.HasPrefix(line, "/")
	line = strings.TrimPrefix(line, "/")
	if strings.Contains(line, "/") {
		anchored = true // a middle separator anchors too
	}
	if line == "" {
		return pattern{}, false
	}

	re, err := regexp.Compile(buildRegex(line, anchored))
	if err != nil {
		return pattern{}, false
	}
	p.re = re
	return p, true
}

// buildRegex anchors the translated glob. An anchored pattern matches from the
// base; an unanchored one matches at any depth (its basename anywhere).
func buildRegex(glob string, anchored bool) string {
	var b strings.Builder
	if anchored {
		b.WriteByte('^')
	} else {
		b.WriteString("(?:^|.*/)")
	}
	b.WriteString(translateGlob(glob))
	b.WriteByte('$')
	return b.String()
}

// translateGlob converts a gitignore glob to an RE2 fragment: * stays within a
// path segment, ** spans segments, ? is a single non-slash, and [...] is a
// character class.
func translateGlob(glob string) string {
	var b strings.Builder
	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i++
				if i+1 < len(runes) && runes[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?") // **/ matches zero or more directories
				} else {
					b.WriteString(".*") // trailing ** matches everything
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '/':
			b.WriteByte('/')
		case '[':
			class, next := charClass(runes, i)
			b.WriteString(class)
			i = next
		default:
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	return b.String()
}

// charClass copies a [...] class starting at open, translating a leading ! to ^.
// It returns the regex class and the index of the closing ]; an unterminated
// class is treated as a literal [.
func charClass(runes []rune, open int) (string, int) {
	i := open + 1
	var b strings.Builder
	b.WriteByte('[')
	if i < len(runes) && (runes[i] == '!' || runes[i] == '^') {
		b.WriteByte('^')
		i++
	}
	if i < len(runes) && runes[i] == ']' {
		b.WriteString(`\]`)
		i++
	}
	for i < len(runes) && runes[i] != ']' {
		if runes[i] == '\\' {
			b.WriteString(`\\`)
		} else {
			b.WriteRune(runes[i])
		}
		i++
	}
	if i >= len(runes) {
		return `\[`, open // unterminated: literal [, resume after it
	}
	b.WriteByte(']')
	return b.String(), i
}
