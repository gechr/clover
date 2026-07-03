package match

import (
	"errors"
	"fmt"

	"github.com/gechr/clover/internal/model"
	xstrings "github.com/gechr/x/strings"
)

// securePin carries the value a secure pin currently pins - a commit SHA or a
// sha256:<hex> digest - for verification against the resolved candidate.
type securePin struct {
	pinned string
}

// Pinned reports the value currently pinned on the line.
func (p securePin) Pinned() string { return p.pinned }

// spliceTwo replaces two located spans on line in one pass - first with
// firstVal, then second with secondVal - erroring rather than half-updating
// when the spans no longer fit the line. It reports whether the result differs
// from line.
func spliceTwo(
	line string,
	first Span,
	firstVal string,
	second Span,
	secondVal string,
) (string, bool, error) {
	if first.Start < 0 || first.End > second.Start || second.End > len(line) {
		return "", false, errors.New("located spans no longer fit the line")
	}
	newLine := line[:first.Start] + firstVal +
		line[first.End:second.Start] + secondVal +
		line[second.End:]
	return newLine, newLine != line, nil
}

// requireCommit errors when candidate carries no full commit SHA to pin.
func requireCommit(candidate model.Candidate) error {
	if !xstrings.IsGitCommit(candidate.Commit) {
		return fmt.Errorf("candidate has no full commit SHA to pin, got %q", candidate.Commit)
	}
	return nil
}

// requireDigest errors when candidate carries no sha256 digest to pin.
func requireDigest(candidate model.Candidate) error {
	if !isDigest(candidate.Digest) {
		return fmt.Errorf("candidate has no sha256 digest to pin, got %q", candidate.Digest)
	}
	return nil
}
