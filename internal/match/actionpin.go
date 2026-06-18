package match

import (
	"errors"
	"fmt"
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
// anchor - a SHA cannot anchor a semver constraint - so a pin without one is an
// error. Render relies on the provider storing the peeled target commit, not an
// annotated-tag object SHA.
type ActionPin struct{}

// NewActionPin returns the action-pin rewriter (stateless value, like Smart).
func NewActionPin() ActionPin { return ActionPin{} }

// Locate parses the action reference, requiring a full 40-hex SHA after @ and a
// version-shaped token in the trailing comment. It errors specifically for each
// way a line can fail to be a secure pin (no reference, not SHA-pinned, short
// SHA, no version comment), so lint can explain the problem.
func (ActionPin) Locate(line string) (Located, error) {
	uses := strings.Index(line, "uses:")
	if uses < 0 {
		return Located{}, errors.New("no uses: action reference on the line")
	}

	at := strings.IndexByte(line[uses:], '@')
	if at < 0 {
		return Located{}, errors.New("action is not pinned by @<sha> (local, docker, or unpinned)")
	}
	at += uses

	start := at + 1
	end := start
	for end < len(line) && xstrings.IsHexChar(rune(line[end])) {
		end++
	}
	if end-start != shaLen {
		return Located{}, errors.New("action pin requires a full 40-character commit SHA")
	}

	hash := strings.IndexByte(line[end:], '#')
	if hash < 0 {
		return Located{}, errors.New(
			"action pin needs a # version comment as the current-version anchor",
		)
	}
	commentStart := end + hash + 1

	tokens := Find(line[commentStart:])
	if len(tokens) == 0 {
		return Located{}, errors.New("action pin version comment has no version")
	}
	token := tokens[0]
	token.Span.Start += commentStart
	token.Span.End += commentStart

	semver, _ := version.Parse(token.Core)
	return Located{
		Raw:    line[token.Span.Start:token.Span.End],
		Semver: semver,
		token:  token,
		commit: Span{Start: start, End: end},
	}, nil
}

// Render replaces the commit SHA with the candidate's commit and the version
// comment with the restyled candidate version, both in one pass. It errors
// rather than half-update when the candidate lacks a usable commit or the
// located spans no longer fit the line.
func (ActionPin) Render(
	line string,
	located Located,
	candidate model.Candidate,
) (string, bool, error) {
	if !xstrings.IsGitCommit(candidate.Commit) {
		return "", false, fmt.Errorf(
			"candidate has no full commit SHA to pin, got %q",
			candidate.Commit,
		)
	}

	commit, comment := located.commit, located.token.Span
	if commit.Start < 0 || commit.End > comment.Start || comment.End > len(line) {
		return "", false, errors.New("located spans no longer fit the line")
	}

	version := restyle(located.token, candidate.Version)
	newLine := line[:commit.Start] + candidate.Commit +
		line[commit.End:comment.Start] + version +
		line[comment.End:]
	return newLine, newLine != line, nil
}
