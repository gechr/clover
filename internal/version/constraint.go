package version

import (
	"fmt"

	"github.com/gechr/clover/internal/constant"
	goversion "github.com/hashicorp/go-version"
)

// Segment indices into a parsed version. go-version pads every version to at
// least three components at parse time, so these are always in range.
const (
	segMajor = 0
	segMinor = 1
)

// Bound is a keyword constraint: a ceiling on how far a candidate may move from
// the current version, expressed in terms of the highest component it may
// change. BoundMajor is effectively unbounded upward.
type Bound int

const (
	BoundMajor Bound = iota // candidate may change any component
	BoundMinor              // candidate must share the current major
	BoundPatch              // candidate must share the current major and minor
)

// kind discriminates the two ways a [Constraint] decides eligibility.
type kind int

const (
	kindKeyword kind = iota // a Bound relative to a current version
	kindRange               // a go-version range expression
)

// Constraint bounds which candidate versions are eligible. It unifies clover's
// two constraint dialects behind a single [Constraint.Allowed] predicate:
//
//   - keyword (major/minor/patch): a [Bound] ceiling relative to the current
//     version - the part go-version cannot express, and clover's reason to own
//     this type at all;
//   - range (>=1.2,<2.0, ~>1.4, =, !=, >, <): delegated to go-version, the
//     Terraform engine, deliberately not npm-style caret/tilde semantics.
//
// A nil *Constraint means unconstrained: [Constraint.Allowed] reports true for
// every candidate. This lets the absent-constraint default flow through the
// selection chain as a no-op without a special case at every call site.
type Constraint struct {
	kind    kind
	bound   Bound
	current *Version
	rng     goversion.Constraints
}

// NewConstraint compiles a constraint expression. A keyword (major/minor/patch)
// yields a [Bound] measured against current; every other expression is parsed
// as a go-version range. A keyword needs a parseable current to measure
// against, so a nil current with a keyword expression fails loud - whereas a
// range ignores current and still works, matching the design's "keyword +
// unparseable current ⇒ fail-loud; range still works".
func NewConstraint(expr string, current *Version) (*Constraint, error) {
	if bound, ok := parseBound(expr); ok {
		if current == nil {
			return nil, fmt.Errorf("constraint %q needs a parseable current version", expr)
		}
		return &Constraint{kind: kindKeyword, bound: bound, current: current}, nil
	}

	rng, err := goversion.NewConstraint(expr)
	if err != nil {
		return nil, fmt.Errorf("parse constraint %q: %w", expr, err)
	}
	return &Constraint{kind: kindRange, rng: rng}, nil
}

// Allowed reports whether candidate satisfies the constraint. A nil receiver is
// unconstrained and allows everything.
func (c *Constraint) Allowed(candidate *Version) bool {
	if c == nil {
		return true
	}

	switch c.kind {
	case kindKeyword:
		return c.allowedByBound(candidate)
	case kindRange:
		return c.rng.Check(candidate)
	}
	return false
}

// allowedByBound enforces the keyword ceiling: a candidate may differ from
// current only in components at or below the bound.
func (c *Constraint) allowedByBound(candidate *Version) bool {
	cur, cand := c.current.Segments(), candidate.Segments()
	switch c.bound {
	case BoundPatch:
		return cur[segMajor] == cand[segMajor] && cur[segMinor] == cand[segMinor]
	case BoundMinor:
		return cur[segMajor] == cand[segMajor]
	case BoundMajor:
		return true
	}
	return false
}

// parseBound maps a keyword expression to its [Bound]. The second result is
// false for any non-keyword expression, signalling NewConstraint to fall
// through to range parsing.
func parseBound(expr string) (Bound, bool) {
	switch expr {
	case constant.ConstraintMajor:
		return BoundMajor, true
	case constant.ConstraintMinor:
		return BoundMinor, true
	case constant.ConstraintPatch:
		return BoundPatch, true
	}
	return BoundMajor, false
}
