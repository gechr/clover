package match

import (
	"errors"
	"strings"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
	xstrings "github.com/gechr/x/strings"
)

// ActionTag rewrites a tag-pinned GitHub Actions reference, converting it to
// the secure pin format - clover is secure by default:
//
//	uses: owner/repo@v4           ->  uses: owner/repo@<40-hex-sha> # v4.2.2
//
// The tag anchors the current version and its constraint; Render replaces it
// with the resolved candidate's commit SHA and appends a comment naming the
// full resolved version, whatever precision the original tag carried. The
// converted line is a secure pin, so subsequent runs route to [ActionPin].
type ActionTag struct{}

// NewActionTag returns the action-tag rewriter (stateless value, like Smart).
func NewActionTag() ActionTag { return ActionTag{} }

// Locate finds the @<tag> version reference of a uses: action, requiring the
// tag to be wholly version-shaped so a branch ref like @main is never treated
// as a version to bump. Stray text after the tag (a trailing comment, extra
// words) is rejected rather than guessed at, since Render appends its own
// version comment.
func (ActionTag) Locate(line string) (Location, error) {
	tag, err := tagSpan(line)
	if err != nil {
		return nil, err
	}

	tokens := Find(line[tag.Start:tag.End])
	if len(tokens) != 1 || tokens[0].Span.Start != 0 || tokens[0].Span.End != tag.End-tag.Start {
		return nil, errors.New("action tag is not a version")
	}
	token := tokens[0]
	token.Span.Start += tag.Start
	token.Span.End += tag.Start

	semver, _ := version.Parse(token.Core)
	return actionTagLocated{
		anchored: anchored{raw: line[token.Span.Start:token.Span.End], semver: semver},
		token:    token,
	}, nil
}

// tagSpan locates the @<tag> reference of a uses: action, returning the span of
// the text after @ up to whitespace or a closing quote. Only an optional
// closing quote and whitespace may follow the tag - stray text means the line
// is not a bare tag pin, so fail rather than convert around it.
func tagSpan(line string) (Span, error) {
	uses := strings.Index(line, "uses:")
	if uses < 0 {
		return Span{}, errors.New("no uses: action reference on the line")
	}
	at := strings.IndexByte(line[uses:], '@')
	if at < 0 {
		return Span{}, errors.New("action carries no @<tag> reference")
	}

	start := uses + at + 1
	end := start
	for end < len(line) && !isSpaceByte(line[end]) && line[end] != '"' && line[end] != '\'' {
		end++
	}
	if end == start {
		return Span{}, errors.New("action carries an empty @<tag> reference")
	}
	after := strings.TrimLeft(strings.TrimSpace(line[end:]), `"'`)
	if !xstrings.IsBlank(after) {
		return Span{}, errors.New("action tag has unexpected text after it")
	}
	return Span{Start: start, End: end}, nil
}

// actionTagLocated is a tag pin awaiting conversion: the version tag span that
// Render replaces with the candidate's commit SHA.
type actionTagLocated struct {
	anchored

	token Token
}

// Rendered reports the version-comment text Render will write for candidate.
// The comment documents the exact version the SHA points at, so it carries the
// candidate's full precision in the conventional v-prefixed form, never the
// original tag's abbreviated style.
func (actionTagLocated) Rendered(candidate model.Candidate) string {
	return defaultVersionStyle(candidate.Version)
}

// Render replaces the tag with the candidate's commit SHA and appends a
// "# vX.Y.Z" comment naming the resolved version, converting the tag pin into
// the secure pin format. It errors rather than half-update when the candidate
// lacks a usable commit or the located span no longer fits the line.
func (l actionTagLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	if err := requireCommit(candidate); err != nil {
		return "", false, err
	}
	tag := l.token.Span
	if tag.Start < 0 || tag.End > len(line) {
		return "", false, errors.New("located tag span no longer fits the line")
	}
	updated := line[:tag.Start] + candidate.Commit + line[tag.End:]
	newLine := strings.TrimRight(updated, " \t") + " # " + defaultVersionStyle(candidate.Version)
	return newLine, newLine != line, nil
}
