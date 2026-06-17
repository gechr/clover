package rule

import (
	"fmt"

	"github.com/gechr/cusp/internal/constant"
	"github.com/gechr/cusp/internal/directive"
	"github.com/gechr/cusp/internal/pattern"
	"github.com/gechr/cusp/internal/version"
)

// Compile turns a directive's rule keys into options for [version.Select].
// current anchors a keyword constraint. It fails loud on an unparseable value or
// a value out of its key's range, so a bad rule is surfaced rather than silently
// ignored.
func Compile(d directive.Directive, current *version.Version) ([]version.Option, error) {
	var opts []version.Option

	if expr, ok := d.Get(constant.RuleConstraint); ok {
		constraint, err := version.NewConstraint(expr, current)
		if err != nil {
			return nil, err
		}
		opts = append(opts, version.WithConstraint(constraint))
	}

	includes, err := predicates(d.All(constant.RuleInclude))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", constant.RuleInclude, err)
	}
	if len(includes) > 0 {
		opts = append(opts, version.WithInclude(includes...))
	}

	excludes, err := predicates(d.All(constant.RuleExclude))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", constant.RuleExclude, err)
	}
	if len(excludes) > 0 {
		opts = append(opts, version.WithExclude(excludes...))
	}

	prerelease, err := d.Bool(constant.RulePrerelease)
	if err != nil {
		return nil, err
	}
	if prerelease {
		opts = append(opts, version.WithPrerelease(true))
	}

	cooldown, err := d.Duration(constant.RuleCooldown)
	if err != nil {
		return nil, err
	}
	if cooldown > 0 {
		opts = append(opts, version.WithCooldown(cooldown))
	}

	behind, err := d.Int(constant.RuleBehind)
	if err != nil {
		return nil, err
	}
	if behind < 0 {
		return nil, fmt.Errorf("%s must be >= 0, got %d", constant.RuleBehind, behind)
	}
	if behind > 0 {
		opts = append(opts, version.WithBehind(behind))
	}

	allowDowngrade, err := d.Bool(constant.RuleAllowDowngrade)
	if err != nil {
		return nil, err
	}
	if allowDowngrade {
		opts = append(opts, version.WithAllowDowngrade(true))
	}

	return opts, nil
}

// predicates compiles each raw include/exclude value into a selection predicate.
func predicates(raws []string) ([]version.Predicate, error) {
	preds := make([]version.Predicate, 0, len(raws))
	for _, raw := range raws {
		p, err := pattern.Compile(raw)
		if err != nil {
			return nil, err
		}
		preds = append(preds, p.Matches)
	}
	return preds, nil
}
