package match

import (
	"errors"

	"github.com/gechr/clover/internal/model"
	xstrings "github.com/gechr/x/strings"
)

// commitLen and sha256Len are the hex lengths of a git commit and a sha256 sum -
// the two hash kinds a follower projects onto its line.
const (
	commitLen = 40
	sha256Len = 64
)

// Hash rewrites a follower's commit or sha256 line by swapping the existing hex
// token for the resolved one. It locates by shape (a 40- or 64-char hex run), so
// it needs no anchor; the resolved value the pipeline passes is spliced in as-is.
type Hash struct{}

// NewHash returns the hash rewriter (stateless value, like Smart).
func NewHash() Hash { return Hash{} }

// Locate finds the single commit- or sha256-length hex run on the line, erroring
// when there is none or more than one (the ambiguity it fails loud on).
func (Hash) Locate(line string) (Located, error) {
	var spans []Span
	for i := 0; i < len(line); {
		if !xstrings.IsHexChar(rune(line[i])) {
			i++
			continue
		}
		j := i
		for j < len(line) && xstrings.IsHexChar(rune(line[j])) {
			j++
		}
		if n := j - i; n == commitLen || n == sha256Len {
			spans = append(spans, Span{Start: i, End: j})
		}
		i = j
	}

	switch len(spans) {
	case 0:
		return nil, errors.New("no commit or sha256 hash on the target line")
	case 1:
		return hashLocated{
			anchored: anchored{raw: line[spans[0].Start:spans[0].End]},
			span:     spans[0],
		}, nil
	default:
		return nil, errors.New("multiple hashes on the line; target is ambiguous")
	}
}

// hashLocated is a single commit- or sha256-length hex span a follower swaps for
// its resolved value. It carries no semver - the value is spliced verbatim.
type hashLocated struct {
	anchored

	span Span
}

// Render splices the resolved value (carried in Candidate.Version) over the
// located hex span.
func (l hashLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	span := l.span
	if span.Start < 0 || span.End > len(line) || span.Start > span.End {
		return "", false, errors.New("located hash span no longer fits the line")
	}
	newLine := line[:span.Start] + candidate.Version + line[span.End:]
	return newLine, newLine != line, nil
}
