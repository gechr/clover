package match

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/model"
)

// DockerTrack rewrites a digest-pinned image reference whose tag is a floating
// ref (latest, nonroot, edge) rather than a version:
//
//	FROM repo:latest@sha256:<64-hex>
//
// Like DockerPin it drives two spans from one candidate - the tag (from
// Candidate.Version) and the digest (from Candidate.Digest) - but it takes the
// tag literally instead of requiring a version-shaped token, so a floating tag
// anchors the line and Semver stays nil. It acts only on already digest-pinned
// lines, never adding a digest.
type DockerTrack struct{}

// NewDockerTrack returns the docker-track rewriter (stateless value, like Smart).
func NewDockerTrack() DockerTrack { return DockerTrack{} }

// Locate finds the literal tag and the @sha256 digest, reusing the digest
// parsing the docker-pin rewriter uses and taking the tag verbatim so a
// non-version tag like "latest" is captured rather than rejected.
func (DockerTrack) Locate(line string) (Location, error) {
	at, digest, err := digestSpan(line)
	if err != nil {
		return nil, err
	}

	tag, err := imageTagLiteral(line[:at])
	if err != nil {
		return nil, err
	}

	return dockerTrackLocated{
		anchored: anchored{raw: line[tag.Start:tag.End], semver: nil},
		tag:      tag,
		digest:   digest,
		pinned:   line[digest.Start:digest.End],
	}, nil
}

// imageTagLiteral returns the span of the tag in a docker image reference - the
// last colon-separated segment after the last slash, taken verbatim (not parsed
// as a version) so a floating tag like "latest" is captured. ref is the part of
// the line before the @ digest, so the tag runs to its end.
func imageTagLiteral(ref string) (Span, error) {
	slash := strings.LastIndexByte(ref, '/')
	colon := strings.LastIndexByte(ref[slash+1:], ':')
	if colon < 0 {
		return Span{}, errors.New("image has no tag to track")
	}
	return Span{Start: slash + 1 + colon + 1, End: len(ref)}, nil
}

// dockerTrackLocated is a tracked digest pin: the literal tag span plus the
// @sha256 digest span, both rewritten from one candidate.
type dockerTrackLocated struct {
	anchored

	tag    Span
	digest Span
	pinned string // the sha256:<hex> digest currently pinned, for verification
}

// Pinned reports the sha256:<hex> content digest currently on the line.
func (l dockerTrackLocated) Pinned() string { return l.pinned }

// NeedsDigest is true: tracking refreshes the content digest the floating tag
// resolves to, so the pipeline resolves one for the chosen candidate.
func (dockerTrackLocated) NeedsDigest() bool { return true }

// Render replaces the tag with the candidate version (the tracked ref, unchanged
// for track=*) and the digest with the candidate's, in one pass. It errors
// rather than half-update when the candidate lacks a digest or the located spans
// no longer fit the line.
func (l dockerTrackLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	if !isDigest(candidate.Digest) {
		return "", false, fmt.Errorf(
			"candidate has no sha256 digest to pin, got %q",
			candidate.Digest,
		)
	}

	tag, digest := l.tag, l.digest
	if tag.Start < 0 || tag.End > digest.Start || digest.End > len(line) {
		return "", false, errors.New("located spans no longer fit the line")
	}

	newLine := line[:tag.Start] + candidate.Version +
		line[tag.End:digest.Start] + candidate.Digest +
		line[digest.End:]
	return newLine, newLine != line, nil
}
