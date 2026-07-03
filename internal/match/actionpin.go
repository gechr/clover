package match

import (
	"errors"
	"strings"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
	xstrings "github.com/gechr/x/strings"
)

// shaLen is the length of a full git commit SHA in hex. Action pins must use the
// full SHA, never an abbreviation, so the pin is unambiguous and tamper-evident.
const shaLen = 40

// ActionPin rewrites a GitHub Actions secure pin, where one resolved candidate
// drives two spans on the same line:
//
//	uses: owner/repo@<40-hex-sha>   # v1.2.3
//
// the commit SHA (from Candidate.Commit) and the trailing version comment (from
// Candidate.Version, restyled). The version comment is the current-version
// anchor - a SHA cannot anchor a semver constraint - so when it is present it
// fixes the version and its style. A pin with no comment is still a valid target:
// it has no current version to anchor a relative constraint, so run resolves it
// per the directive (latest unless a range constrains it) and Render appends a
// fresh comment, documenting the version the SHA now points at. Render relies on
// the provider storing the peeled target commit, not an annotated-tag object SHA.
type ActionPin struct{}

// NewActionPin returns the action-pin rewriter (stateless value, like Smart).
func NewActionPin() ActionPin { return ActionPin{} }

// Locate parses the action reference, requiring a full 40-hex SHA after @. A
// version-shaped token in the trailing comment, when present, anchors the current
// version and its style. A pin with no comment at all is located with no current
// version, so run resolves it fresh and Render appends the comment. It errors for
// each way the line fails to be a SHA pin (no reference, not SHA-pinned, short
// SHA), and when a comment is present but carries no version - clover will not
// guess whether a human note like "# pinned" was meant to be a version.
func (ActionPin) Locate(line string) (Location, error) {
	commit, end, err := commitSpan(line)
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
		after := strings.TrimSpace(line[end:])
		if strings.TrimSpace(strings.TrimLeft(after, `"'`)) != "" {
			return nil, errors.New("action pin has unexpected text after the commit SHA")
		}
		return actionPinLocated{
			securePin: securePin{pinned: line[commit.Start:commit.End]},
			commit:    commit,
		}, nil
	}
	commentStart := end + hash + 1

	tokens := Find(line[commentStart:])
	if len(tokens) == 0 {
		return nil, errors.New("action pin version comment has no version")
	}
	token := tokens[0]
	token.Span.Start += commentStart
	token.Span.End += commentStart

	semver, _ := version.Parse(token.Core)
	return actionPinLocated{
		anchored:   anchored{raw: line[token.Span.Start:token.Span.End], semver: semver},
		securePin:  securePin{pinned: line[commit.Start:commit.End]},
		token:      token,
		commit:     commit,
		hasComment: true,
	}, nil
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

// actionPinLocated is a secure action pin: the commit SHA span plus the trailing
// version-comment token, both rewritten from one candidate. hasComment is false
// for an undocumented pin, whose comment Render synthesises rather than replaces.
type actionPinLocated struct {
	anchored
	securePin

	token      Token
	commit     Span
	hasComment bool
}

// Rendered reports the version-comment text Render will write for candidate -
// the restyled current version, so the report shows what lands on the line
// (e.g. v7.0.0) rather than the upstream tag's bare core (e.g. 7). An undocumented
// pin has no style to preserve, so it gets the default v-prefixed form.
func (l actionPinLocated) Rendered(candidate model.Candidate) string {
	if !l.hasComment {
		return defaultVersionStyle(candidate.Version)
	}
	return restyle(l.token, candidate.Version)
}

// Render rewrites the commit SHA with the candidate's commit and, in one pass,
// either replaces the existing version comment with the restyled candidate
// version or - for an undocumented pin - appends a fresh one. It errors rather
// than half-update when the candidate lacks a usable commit or the located spans
// no longer fit the line.
func (l actionPinLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
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
func (l actionPinLocated) appendComment(
	line string,
	candidate model.Candidate,
) (string, bool, error) {
	commit := l.commit
	if commit.Start < 0 || commit.End > len(line) {
		return "", false, errors.New("located commit span no longer fits the line")
	}
	updated := line[:commit.Start] + candidate.Commit + line[commit.End:]
	newLine := strings.TrimRight(updated, " \t") + " # " + defaultVersionStyle(candidate.Version)
	return newLine, newLine != line, nil
}

// defaultVersionStyle styles a version for a freshly added action-pin comment.
// GitHub action tags are conventionally v-prefixed, so a pin documented for the
// first time leads with v at whatever precision the resolved tag carries.
func defaultVersionStyle(v string) string {
	return "v" + strings.TrimPrefix(v, "v")
}
