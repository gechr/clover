package match

import (
	"errors"
	"strings"

	"github.com/gechr/clover/internal/version"
)

// DockerTag rewrites a tag-only image reference, where the version is the tag of
// an image ref:
//
//	FROM localhost:5000/team/api:2.0.1
//	image: 123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:2.0.1
//
// Unlike the smart rewriter it does not scan the whole line - a registry :port,
// an ECR account id, or a region like us-east-1 are all version-shaped and would
// make the line ambiguous. Instead it anchors on the image reference, taking the
// tag as the last colon-segment after the last slash, so only the tag is read.
// Rendering is the smart restyle, so DockerTag embeds [Smart] for Render.
type DockerTag struct{ Smart }

// NewDockerTag returns the docker tag-only rewriter (stateless value, like Smart).
func NewDockerTag() DockerTag { return DockerTag{} }

// Locate finds the version token in the image tag, ignoring the registry host,
// port, and path so they are never mistaken for the version.
func (DockerTag) Locate(line string) (Located, error) {
	token, err := imageTag(line)
	if err != nil {
		return Located{}, err
	}
	semver, _ := version.Parse(token.Core)
	return Located{
		Raw:    line[token.Span.Start:token.Span.End],
		Semver: semver,
		token:  token,
	}, nil
}

// imageTag locates the version token in the tag of a docker image reference
// occupying ref - the whole line for a tag-only image, or the part before @ for
// a digest pin. The tag is the last colon-separated segment after the last
// slash, so a registry's :port is not mistaken for it; the returned token's span
// is relative to ref (and so to the line, since ref is a prefix of it).
func imageTag(ref string) (Token, error) {
	slash := strings.LastIndexByte(ref, '/')
	colon := strings.LastIndexByte(ref[slash+1:], ':')
	if colon < 0 {
		return Token{}, errors.New("image has no tag to anchor the version")
	}
	tagStart := slash + 1 + colon + 1

	tokens := Find(ref[tagStart:])
	if len(tokens) != 1 {
		return Token{}, errors.New("image tag is not a single version")
	}
	token := tokens[0]
	token.Span.Start += tagStart
	token.Span.End += tagStart
	return token, nil
}
