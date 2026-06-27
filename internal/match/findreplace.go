package match

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pattern"
	"github.com/gechr/clover/internal/version"
)

// FindReplace is the explicit find/replace rewriter. find locates the region to
// rewrite: a glob with <placeholders> (each a capturing group of its shape), or
// a /regex/ (strict, no placeholders; group 1 is the value). replace is optional:
// without it each captured value is substituted in place, preserving the rest;
// with it the whole matched region is re-rendered from the template. A token
// renders its resolved value when clover computes one, else the text find
// captured, so a match-only <hex> is preserved.
type FindReplace struct {
	find    *regexp.Regexp
	replace string
	tokens  pattern.Tokens // capture-group token names; nil in regex mode
}

// NewFindReplace compiles a find/replace pair through the shared pattern grammar:
// a /regex/ find is strict (nil tokens), any other find is a glob with
// placeholders.
func NewFindReplace(find, replace string) (FindReplace, error) {
	pat, err := pattern.Compile(find)
	if err != nil {
		return FindReplace{}, fmt.Errorf("find: %w", err)
	}
	return FindReplace{find: pat.Regexp(), tokens: pat.Tokens(), replace: replace}, nil
}

// Locate matches find against the line: the whole match is the span to rewrite,
// and the value anchoring selection is the first version-shaped capture (glob)
// or capture group 1 (regex), falling back to the whole match.
func (fr FindReplace) Locate(line string) (Location, error) {
	m := fr.find.FindStringSubmatchIndex(line)
	if m == nil {
		return nil, errors.New("find pattern did not match the target line")
	}
	value := line[fr.anchor(m, 0):fr.anchor(m, 1)]
	semver, _ := version.Parse(value)
	return findReplaceLocated{
		anchored: anchored{raw: value, semver: semver},
		fr:       fr,
		match:    m,
	}, nil
}

// findReplaceLocated carries the match indices from Locate, so Render rewrites
// the matched region without re-running the regex, plus the rewriter that holds
// the replace template and token names.
type findReplaceLocated struct {
	anchored

	fr    FindReplace
	match []int
}

// Render rewrites the matched region: in place per captured token, or from the
// replace template when one is set.
func (l findReplaceLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	m := l.match
	if m[0] < 0 || m[1] > len(line) {
		return "", false, errors.New("located span no longer fits the line")
	}
	region, err := l.fr.region(line, m, l.raw, candidate)
	if err != nil {
		return "", false, err
	}
	newLine := line[:m[0]] + region + line[m[1]:]
	return newLine, newLine != line, nil
}

// anchor returns the start (end=0) or end (end=1) offset of the value used as the
// selection anchor: the first version-family capture, else group 1, else the
// whole match.
func (fr FindReplace) anchor(m []int, end int) int {
	if g := fr.versionGroup(); g > 0 && m[2*g] >= 0 {
		return m[2*g+end]
	}
	if len(m) >= 4 && m[2] >= 0 {
		return m[2+end]
	}
	return m[end]
}

// versionGroup is the 1-based capture index of the first version-family token,
// or 0 when there is none (a commit- or hex-only find has no semver anchor).
func (fr FindReplace) versionGroup() int {
	for i, t := range fr.tokens {
		//nolint:exhaustive // version-family subset; other tokens intentionally fall through.
		switch t {
		case pattern.TokenVersion,
			pattern.TokenMajor,
			pattern.TokenMinor,
			pattern.TokenPatch,
			pattern.TokenMajorMinor,
			pattern.TokenMajorMinorPatch:
			return i + 1
		}
	}
	return 0
}

// region builds the new content for the matched span.
func (fr FindReplace) region(
	line string,
	m []int,
	raw string,
	candidate model.Candidate,
) (string, error) {
	if fr.replace != "" {
		out := pattern.Expand(fr.replace, fr.values(line, m, raw, candidate))
		if pattern.HasToken(out) {
			return "", fmt.Errorf("replace %q references an unavailable token", fr.replace)
		}
		return out, nil
	}

	// In place: regex mode rewrites just the value span; glob mode rewrites each
	// captured token, preserving the literal/glob text between captures.
	if fr.tokens == nil {
		vs, ve := fr.anchor(m, 0), fr.anchor(m, 1)
		styled := restyle(decompose(line[vs:ve]), candidate.Version)
		return line[m[0]:vs] + styled + line[ve:m[1]], nil
	}
	var b strings.Builder
	cursor := m[0]
	for i, t := range fr.tokens {
		gs, ge := m[2*(i+1)], m[2*(i+1)+1]
		if gs < 0 {
			continue
		}
		b.WriteString(line[cursor:gs])
		b.WriteString(tokenValue(t, line[gs:ge], candidate))
		cursor = ge
	}
	b.WriteString(line[cursor:m[1]])
	return b.String(), nil
}

// values maps every token referenced anywhere to its value: the resolved value
// when clover computes one, else the text find captured for that token (so a
// replace can echo a matched <hex>).
func (fr FindReplace) values(
	line string,
	m []int,
	raw string,
	candidate model.Candidate,
) pattern.TokenMap {
	vals := resolved(raw, candidate)
	for i, t := range fr.tokens {
		if _, ok := vals[t]; ok {
			continue
		}
		if gs := m[2*(i+1)]; gs >= 0 {
			vals[t] = line[gs:m[2*(i+1)+1]]
		}
	}
	return vals
}

// resolved is the map of tokens clover computes from the candidate, styling
// <version> to the located value.
func resolved(located string, candidate model.Candidate) pattern.TokenMap {
	//nolint:exhaustive // built incrementally below; only the computable tokens are set.
	vals := pattern.TokenMap{pattern.TokenVersion: restyle(decompose(located), candidate.Version)}
	if seg := segments(candidate.Semver); seg != nil {
		vals[pattern.TokenMajor], vals[pattern.TokenMinor], vals[pattern.TokenPatch] = seg[0], seg[1], seg[2]
		vals[pattern.TokenMajorMinor] = seg[0] + "." + seg[1]
		vals[pattern.TokenMajorMinorPatch] = strings.Join(seg, ".")
	}
	if candidate.Commit != "" {
		vals[pattern.TokenCommit] = candidate.Commit
	}
	if d, ok := strings.CutPrefix(candidate.Digest, constant.DigestSha256); ok {
		vals[pattern.TokenSHA256] = d
	}
	return vals
}

// tokenValue is the resolved value for an in-place token, styling <version> to
// the captured text, or the captured text itself when nothing is resolved.
func tokenValue(t pattern.Token, captured string, candidate model.Candidate) string {
	if v, ok := resolved(captured, candidate)[t]; ok {
		return v
	}
	return captured
}

// segments returns the major, minor, patch of a parsed version as strings, or
// nil when it is not semver-shaped.
func segments(v *version.Version) []string {
	if v == nil {
		return nil
	}
	seg := v.Segments() // go-version pads to three: major, minor, patch
	out := make([]string, len(seg))
	for i, s := range seg {
		out[i] = strconv.Itoa(s)
	}
	return out
}
