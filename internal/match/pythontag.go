package match

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/version"
)

// pyTag matches the compact Python target ruff, black, and mypy write in
// pyproject.toml: py<major><minor> (py39, py310, py314), the first digit the
// major and the rest the minor.
var pyTag = regexp.MustCompile(`\bpy(\d)(\d+)\b`)

// PythonTag rewrites a compact Python target-version token - py314 for Python
// 3.14. The token is not version-shaped (no dots), so the smart rewriter cannot
// see it; this rewriter parses the major and minor out of the pyXY form, anchors
// selection on that line, and renders the resolved version back into the same
// compact form (dropping patch and prerelease, which the form does not carry).
type PythonTag struct{}

// NewPythonTag returns the compact Python target-version rewriter (stateless
// value, like the other format rewriters).
func NewPythonTag() PythonTag { return PythonTag{} }

// Locate finds the single py<major><minor> token on the line. It errors when
// none is present, or when more than one is - an array like
// target-version = ["py311", "py312"] is ambiguous, so it is left alone.
func (PythonTag) Locate(line string) (Location, error) {
	matches := pyTag.FindAllStringSubmatchIndex(line, -1)
	switch len(matches) {
	case 0:
		return nil, errors.New("no py<major><minor> target on the line")
	case 1:
		// The single match anchors the rewrite.
	default:
		return nil, errors.New("multiple py targets, so it is ambiguous which to track")
	}

	m := matches[0]
	raw := line[m[0]:m[1]]
	semver, _ := version.Parse(line[m[2]:m[3]] + "." + line[m[4]:m[5]])
	return pythonTagLocated{
		anchored: anchored{raw: raw, semver: semver},
		start:    m[0],
		end:      m[1],
	}, nil
}

// pythonTagLocated carries the matched py-token span so Render rewrites it
// without re-running the regex.
type pythonTagLocated struct {
	anchored

	start, end int
}

// Rendered reports the compact form Render will write, so the report matches the
// file.
func (l pythonTagLocated) Rendered(candidate model.Candidate) string {
	return pyForm(candidate)
}

// Render rewrites the py-token to the resolved candidate's compact form. It is a
// no-op when the minor line is unchanged (py314 -> py314 for a 3.14 patch bump),
// and errors when the candidate has no parseable semver or the span no longer fits.
func (l pythonTagLocated) Render(line string, candidate model.Candidate) (string, bool, error) {
	if l.start < 0 || l.end > len(line) {
		return "", false, errors.New("located py target span no longer fits the line")
	}
	rendered := pyForm(candidate)
	if rendered == "" {
		return "", false, fmt.Errorf("cannot render a py target for %q", candidate.Version)
	}
	if rendered == line[l.start:l.end] {
		return line, false, nil
	}
	return line[:l.start] + rendered + line[l.end:], true, nil
}

// pyForm renders a candidate as py<major><minor>, dropping patch and prerelease:
// the compact form carries only the minor line. Empty when the candidate has no
// parseable semver.
func pyForm(candidate model.Candidate) string {
	v := candidate.Semver
	if v == nil {
		v, _ = version.Parse(candidate.Version)
	}
	if v == nil {
		return ""
	}
	seg := v.Segments()
	return fmt.Sprintf("py%d%d", seg[0], seg[1])
}
