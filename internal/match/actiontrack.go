package match

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/model"
	xstrings "github.com/gechr/x/strings"
)

// ActionTrack rewrites a GitHub Actions secure pin whose trailing comment names
// a floating branch rather than a version:
//
//	uses: owner/repo@<40-hex-sha>   # main
//
// Like ActionPin it drives two spans from one candidate - the commit SHA (from
// Candidate.Commit) and the comment (from Candidate.Version) - but it takes the
// comment literally instead of requiring a version-shaped token, so a branch
// name anchors the line and Semver stays nil.
type ActionTrack struct{}

// NewActionTrack returns the action-track rewriter (stateless value, like Smart).
func NewActionTrack() ActionTrack { return ActionTrack{} }

// Locate finds the @<sha> commit and the # comment, reusing the SHA parsing the
// action-pin rewriter uses and taking the comment verbatim so a branch name like
// "main" is captured rather than rejected.
func (ActionTrack) Locate(line string) (Location, error) {
	commit, end, err := commitSpan(line)
	if err != nil {
		return nil, err
	}

	comment, err := actionComment(line, end)
	if err != nil {
		return nil, err
	}

	return actionTrackLocated{
		anchored: anchored{raw: line[comment.Start:comment.End], semver: nil},
		comment:  comment,
		commit:   commit,
		pinned:   line[commit.Start:commit.End],
	}, nil
}

// actionComment returns the span of the first whitespace-delimited word of the #
// comment after the commit SHA - the tracked branch name - erroring when the pin
// carries no comment to anchor the branch.
func actionComment(line string, after int) (Span, error) {
	hash := strings.IndexByte(line[after:], '#')
	if hash < 0 {
		return Span{}, errors.New("action pin needs a # comment naming the branch")
	}
	start := after + hash + 1
	for start < len(line) && isSpaceByte(line[start]) {
		start++
	}
	end := start
	for end < len(line) && !isSpaceByte(line[end]) {
		end++
	}
	if end == start {
		return Span{}, errors.New("action pin # comment names no branch")
	}
	return Span{Start: start, End: end}, nil
}

// isSpaceByte reports whether b is an ASCII space or tab.
func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' }

// actionTrackLocated is a tracked action pin: the commit SHA span plus the
// literal branch comment, both rewritten from one candidate.
type actionTrackLocated struct {
	anchored

	comment Span
	commit  Span
	pinned  string // the commit SHA currently pinned, for verification
}

// Pinned reports the commit SHA currently on the line.
func (l actionTrackLocated) Pinned() string { return l.pinned }

// Render replaces the commit SHA with the candidate's commit and the comment
// with the candidate version (the tracked branch, unchanged for track=*), both
// in one pass. It errors rather than half-update when the candidate lacks a
// usable commit or the located spans no longer fit the line.
func (l actionTrackLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	if !xstrings.IsGitCommit(candidate.Commit) {
		return "", false, fmt.Errorf(
			"candidate has no full commit SHA to pin, got %q",
			candidate.Commit,
		)
	}

	commit, comment := l.commit, l.comment
	if commit.Start < 0 || commit.End > comment.Start || comment.End > len(line) {
		return "", false, errors.New("located spans no longer fit the line")
	}

	newLine := line[:commit.Start] + candidate.Commit +
		line[commit.End:comment.Start] + candidate.Version +
		line[comment.End:]
	return newLine, newLine != line, nil
}
