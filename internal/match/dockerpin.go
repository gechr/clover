package match

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
	xstrings "github.com/gechr/x/strings"
)

const (
	digestAlgo   = "sha256:"
	digestHexLen = 64
)

// DockerPin rewrites a digest-pinned image reference, where one resolved
// candidate drives two spans on the same line:
//
//	FROM repo:1.27@sha256:<64-hex>
//
// the tag (restyled from Candidate.Version) and the digest (from
// Candidate.Digest). The tag is the current-version anchor, so a pin without one
// is an error; it acts only on already-pinned lines, never adding a digest.
type DockerPin struct{}

// NewDockerPin returns the docker-pin rewriter (stateless value, like Smart).
func NewDockerPin() DockerPin { return DockerPin{} }

// Locate finds the version tag and the @sha256 digest, erroring specifically for
// each way a line can fail to be a digest pin (not pinned, short or non-sha256
// digest, no tag, non-version tag) so lint can explain it.
func (DockerPin) Locate(line string) (Located, error) {
	at := strings.LastIndexByte(line, '@')
	if at < 0 {
		return nil, errors.New("image is not digest-pinned by @sha256")
	}

	rest := line[at+1:]
	if !strings.HasPrefix(rest, digestAlgo) {
		return nil, errors.New("image pin digest must be sha256")
	}
	hexStart := at + 1 + len(digestAlgo)
	hexEnd := hexStart
	for hexEnd < len(line) && xstrings.IsHexChar(rune(line[hexEnd])) {
		hexEnd++
	}
	if hexEnd-hexStart != digestHexLen {
		return nil, errors.New("image pin requires a full sha256 digest")
	}

	// The tag anchors the current version: the same image-ref parsing the
	// tag-only rewriter uses, scoped to the reference before the @ digest.
	token, err := imageTag(line[:at])
	if err != nil {
		return nil, err
	}

	semver, _ := version.Parse(token.Core)
	return dockerPinLocated{
		anchored: anchored{raw: line[token.Span.Start:token.Span.End], semver: semver},
		token:    token,
		digest:   Span{Start: at + 1, End: hexEnd},
	}, nil
}

// dockerPinLocated is a digest pin: the version tag plus the @sha256 digest span,
// both rewritten from one candidate.
type dockerPinLocated struct {
	anchored

	token  Token
	digest Span
}

// NeedsDigest is true: a pin always rewrites its content digest, so the pipeline
// resolves one for the chosen candidate.
func (dockerPinLocated) NeedsDigest() bool { return true }

// Render replaces the tag with the restyled candidate version and the digest
// with the candidate's, in one pass. It errors rather than half-update when the
// candidate lacks a digest or the located spans no longer fit the line.
func (l dockerPinLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	if !isDigest(candidate.Digest) {
		return "", false, fmt.Errorf(
			"candidate has no sha256 digest to pin, got %q",
			candidate.Digest,
		)
	}

	tag, digest := l.token.Span, l.digest
	if tag.Start < 0 || tag.End > digest.Start || digest.End > len(line) {
		return "", false, errors.New("located spans no longer fit the line")
	}

	version := restyle(l.token, candidate.Version)
	newLine := line[:tag.Start] + version +
		line[tag.End:digest.Start] + candidate.Digest +
		line[digest.End:]
	return newLine, newLine != line, nil
}

// isDigest reports whether s is a full sha256:<64-hex> content digest.
func isDigest(s string) bool {
	rest, ok := strings.CutPrefix(s, digestAlgo)
	return ok && xstrings.IsSHA256(rest)
}
