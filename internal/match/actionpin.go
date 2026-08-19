package match

import (
	"errors"
	"regexp"
	"strings"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
	xstrings "github.com/gechr/x/strings"
)

// shaLen is the length of a full git commit SHA in hex. Action pins must use the
// full SHA, never an abbreviation, so the pin is unambiguous and tamper-evident.
const shaLen = 40

// shaLocator finds the commit SHA a pin carries, returning its span and the
// index just past it, where a trailing version comment is searched for. It is
// the one thing that differs between the line syntaxes [CommitPin] serves.
type shaLocator func(line string) (Span, int, error)

// CommitPin rewrites a secure pin whose commit SHA is documented by a trailing
// version comment, where one resolved candidate drives both spans:
//
//	uses: owner/repo@<40-hex-sha>   # v1.2.3
//	rev: <40-hex-sha>               # frozen: 22.3.0
//
// the commit SHA (from Candidate.Commit) and the comment (from
// Candidate.Version, restyled). The version comment is the current-version
// anchor - a SHA cannot anchor a semver constraint - so when it is present it
// fixes the version and its style. A pin with no comment is still a valid target:
// it has no current version to anchor a relative constraint, so run resolves it
// per the directive (latest unless a range constrains it) and Render appends a
// fresh comment, documenting the version the SHA now points at. Render relies on
// the provider storing the peeled target commit, not an annotated-tag object SHA.
//
// The two syntaxes are one rewriter rather than two because they differ only in
// where the SHA sits and what a synthesized comment reads - never in what is
// written or from which candidate field. That is why this is parameterized while
// [DockerPin], which pins a different value from a different field, is separate.
type CommitPin struct {
	// locate finds the SHA on the line.
	locate shaLocator
	// comment renders the comment body written for a pin that carries none,
	// after the "# ".
	comment func(version string) string
}

// NewActionPin returns the rewriter for a GitHub Actions secure pin, whose
// comment is conventionally the v-prefixed tag.
func NewActionPin() CommitPin {
	return CommitPin{locate: commitSpan, comment: defaultVersionStyle}
}

// NewPreCommitPin returns the rewriter for the frozen rev `pre-commit
// autoupdate --freeze` writes, whose comment carries the frozen: marker and the
// tag exactly as the hook repository spells it - hook tags are as often bare
// (22.3.0) as v-prefixed, so no prefix is imposed.
func NewPreCommitPin() CommitPin {
	return CommitPin{
		locate:  revCommitSpan,
		comment: func(version string) string { return frozenMarker + " " + version },
	}
}

// frozenMarker introduces the version comment on a frozen pre-commit rev.
const frozenMarker = "frozen:"

// Locate parses the action reference, requiring a full 40-hex SHA after @. A
// version-shaped token in the trailing comment, when present, anchors the current
// version and its style. A pin with no comment at all is located with no current
// version, so run resolves it fresh and Render appends the comment. It errors for
// each way the line fails to be a SHA pin (no reference, not SHA-pinned, short
// SHA), and when a comment is present but carries no version - clover will not
// guess whether a human note like "# pinned" was meant to be a version.
func (p CommitPin) Locate(line string) (Location, error) {
	commit, end, err := p.locate(line)
	if err != nil {
		return nil, err
	}

	hash := strings.IndexByte(line[end:], '#')
	if hash < 0 {
		// An undocumented pin: a valid target whose version run will resolve and
		// Render will append. No comment means no current-version anchor. Only an
		// optional closing quote and whitespace may follow the SHA - stray text
		// (uses: …@<sha> extra) is malformed, so fail rather than append a comment
		// after the garbage.
		if !xstrings.IsBlank(strings.TrimLeft(line[end:], `"'`)) {
			return nil, errors.New("action pin has unexpected text after the commit SHA")
		}
		return commitPinLocated{
			pinned:  line[commit.Start:commit.End],
			commit:  commit,
			comment: p.comment,
		}, nil
	}
	commentStart := end + hash + 1

	token, ok := commentToken(line[commentStart:])
	if !ok {
		return nil, errors.New("action pin version comment has no version")
	}
	token.Span.Start += commentStart
	token.Span.End += commentStart

	semver, _ := version.Parse(token.Core)
	return commitPinLocated{
		raw:        line[token.Span.Start:token.Span.End],
		semver:     semver,
		pinned:     line[commit.Start:commit.End],
		token:      token,
		commit:     commit,
		comment:    p.comment,
		hasComment: true,
	}, nil
}

// commentToken selects the version token in the trailing comment region, the
// text after the first # following the SHA. That region may hold more than one
// #-delimited comment - a directive such as `# renovate: …` sitting beside the
// version - and position alone does not say which is the pin's version: taking
// the first token would read one out of a leading directive, taking the last
// would read one out of a trailing note.
//
// A comment whose whole body is a single version token is unambiguous, so the
// first such segment wins wherever it sits. Only when no segment qualifies does
// it fall back to the first token anywhere in the region, preserving the
// behaviour for a decorated comment like `# v1.2.3 (pinned)`.
func commentToken(region string) (Token, bool) {
	for start := 0; start <= len(region); {
		seg, next := region[start:], -1
		if i := strings.IndexByte(seg, '#'); i >= 0 {
			seg, next = seg[:i], start+i+1
		}
		if token, ok := soleToken(seg); ok {
			token.Span.Start += start
			token.Span.End += start
			return token, true
		}
		if next < 0 {
			break
		}
		start = next
	}

	tokens := Find(region)
	if len(tokens) == 0 {
		return Token{}, false
	}
	return tokens[0], true
}

// soleToken returns the token when seg is wholly one version token, i.e. only
// whitespace surrounds it. A segment carrying any other text is prose, not a
// version comment, so it is not treated as the anchor.
func soleToken(seg string) (Token, bool) {
	tokens := Find(seg)
	if len(tokens) != 1 {
		return Token{}, false
	}
	token := tokens[0]
	if !xstrings.IsBlank(seg[:token.Span.Start]) || !xstrings.IsBlank(seg[token.Span.End:]) {
		return Token{}, false
	}
	return token, true
}

// commitSpan locates the @<40-hex> commit SHA of a uses: action reference,
// returning the SHA span and the index just past it (where a trailing comment is
// searched for), with an error specific to each way the line fails to be
// SHA-pinned. Shared by the action-pin and action-track rewriters.
func commitSpan(line string) (Span, int, error) {
	uses := strings.Index(line, "uses:")
	if uses < 0 {
		return Span{}, 0, errors.New("no uses: action reference on the line")
	}
	at := strings.IndexByte(line[uses:], '@')
	if at < 0 {
		return Span{}, 0, errors.New("action is not pinned by @<sha> (local, docker, or unpinned)")
	}
	at += uses

	start := at + 1
	end := start
	for end < len(line) && xstrings.IsHexChar(rune(line[end])) {
		end++
	}
	if end-start != shaLen {
		return Span{}, 0, errors.New("action pin requires a full 40-character commit SHA")
	}
	return Span{Start: start, End: end}, end, nil
}

// commitPinLocated is a located secure pin: the commit SHA span plus the
// trailing version-comment token, both rewritten from one candidate. hasComment
// is false for an undocumented pin, whose comment Render synthesises rather than
// replaces, using the syntax's own comment renderer.
type commitPinLocated struct {
	anchored
	securePin

	token      Token
	commit     Span
	comment    func(version string) string
	hasComment bool
}

// Rendered reports the version-comment text Render will write for candidate -
// the restyled current version, so the report shows what lands on the line
// (e.g. v7.0.0) rather than the upstream tag's bare core (e.g. 7). An undocumented
// pin has no style to preserve, so it gets the default v-prefixed form.
func (l commitPinLocated) Rendered(candidate model.Candidate) string {
	if !l.hasComment {
		return l.comment(candidate.Version)
	}
	return restyle(l.token, candidate.Version)
}

// Render rewrites the commit SHA with the candidate's commit and, in one pass,
// either replaces the existing version comment with the restyled candidate
// version or - for an undocumented pin - appends a fresh one. It errors rather
// than half-update when the candidate lacks a usable commit or the located spans
// no longer fit the line.
func (l commitPinLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	if err := requireCommit(candidate); err != nil {
		return "", false, err
	}
	if !l.hasComment {
		return l.appendComment(line, candidate)
	}
	return spliceTwo(
		line,
		l.commit,
		candidate.Commit,
		l.token.Span,
		restyle(l.token, candidate.Version),
	)
}

// appendComment rewrites the SHA and adds a "# vX.Y.Z" version comment to a pin
// that had none, documenting the version run resolved. Trailing whitespace is
// trimmed first so the comment sits one space after the reference.
func (l commitPinLocated) appendComment(
	line string,
	candidate model.Candidate,
) (string, bool, error) {
	commit := l.commit
	if commit.Start < 0 || commit.End > len(line) {
		return "", false, errors.New("located commit span no longer fits the line")
	}
	updated := line[:commit.Start] + candidate.Commit + line[commit.End:]
	newLine := strings.TrimRight(updated, " \t") + " # " + l.comment(candidate.Version)
	return newLine, newLine != line, nil
}

// defaultVersionStyle styles a version for a freshly added action-pin comment.
// GitHub action tags are conventionally v-prefixed, so a pin documented for the
// first time leads with v at whatever precision the resolved tag carries.
func defaultVersionStyle(v string) string {
	return "v" + strings.TrimPrefix(v, "v")
}

// revCommitSpan locates the commit SHA of a frozen pre-commit rev, returning the
// SHA span and the index just past it. `pre-commit autoupdate --freeze` writes
// the SHA as the rev's scalar value, so it is found after the key rather than
// after an @ separator:
//
//	rev: 552baf822992936134cbd31a38f69c8cfe7c0f05  # frozen: 22.3.0
//
// An optional quote is stepped over, since a YAML scalar may be quoted. The key
// is matched with the same tolerance as the route that dispatches here - YAML
// permits whitespace before the colon and strips it from the key - so a line the
// route claims is always one this can read. The errors mirror the action
// locator's, so lint explains each way a line fails to be a frozen pin.
func revCommitSpan(line string) (Span, int, error) {
	key := revKey.FindStringIndex(line)
	if key == nil {
		return Span{}, 0, errors.New("no rev: pin on the line")
	}

	start := key[1]
	for start < len(line) && (line[start] == ' ' || line[start] == '\t') {
		start++
	}
	if start < len(line) && (line[start] == '"' || line[start] == '\'') {
		start++
	}

	end := start
	for end < len(line) && xstrings.IsHexChar(rune(line[end])) {
		end++
	}
	if end-start != shaLen {
		return Span{}, 0, errors.New("rev is not pinned by a full 40-character commit SHA")
	}
	return Span{Start: start, End: end}, end, nil
}

// revKey matches the rev mapping key and its colon, tolerating the whitespace
// before it that YAML permits, so this agrees with the route's own pattern.
var revKey = regexp.MustCompile(`\brev\s*:`)
