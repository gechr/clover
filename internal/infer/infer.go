// Package infer bridges auto-detection and the provider registry: it decides
// whether a line Clover recognizes would actually resolve, offline. The one
// gate is shared by annotate, which writes a directive above each passing
// line, and by run --infer, which synthesizes a marker for it without writing
// anything - so the two features can never disagree about what is trackable.
package infer

import (
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/match"
	"github.com/gechr/clover/internal/provider"
)

// Recognize reports the inference for lines[i] and whether its line would
// resolve: ok is false when no auto route matches at all, and reason carries
// why a matched line still cannot resolve (an incomplete reference, a provider
// resource that does not build, a version the rewriter cannot locate). A
// matched line with an empty reason is safe to annotate or update.
func Recognize(path string, lines []string, i int) (match.Inference, string, bool) {
	inf, ok := match.Infer(path, lines, i)
	if !ok {
		return match.Inference{}, "", false
	}
	if reason := inf.Missing(); reason != "" {
		return inf, reason, true
	}
	return inf, unresolvedReason(path, inf, lines[i]), true
}

// Recognizable reports whether lines[i] names a complete reference, stopping
// before the heavier offline resolution gate. It backs opt-out diagnostics,
// which only need to know a line would otherwise have been a candidate.
func Recognizable(path string, lines []string, i int) bool {
	inf, ok := match.Infer(path, lines, i)
	return ok && inf.Missing() == ""
}

// unresolvedReason reports why the directive a provider=auto marker would bind
// for this line fails the offline checks lint runs: the inferred provider must
// exist, build a valid resource, and locate a trackable version. An inferred
// track selects the docker-track rewriter, mirroring the run pipeline's
// dispatch for track markers.
func unresolvedReason(path string, inf match.Inference, line string) string {
	return Unresolved(inf.Provider, Directive(inf), line,
		func() (match.Rewriter, error) {
			if inf.Track != "" {
				return match.NewDockerTrack(), nil
			}
			return match.For(match.Context{Path: path, Line: line, Provider: inf.Provider}), nil
		})
}

// Unresolved runs the offline checks lint and run perform against a candidate
// annotation: the provider must exist, build a valid resource from d, and the
// rewriter must locate a trackable version on line. An empty reason means the
// candidate is safe to emit.
func Unresolved(
	providerName string,
	d directive.Directive,
	line string,
	rewriter func() (match.Rewriter, error),
) string {
	prov, ok := provider.Get(providerName)
	if !ok {
		return "unknown provider"
	}
	if _, err := prov.Resource(d); err != nil {
		return err.Error()
	}
	rw, err := rewriter()
	if err != nil {
		return err.Error()
	}
	if _, err = rw.Locate(line); err != nil {
		return err.Error()
	}
	return ""
}

// Directive builds the directive a provider=auto marker binds for inf: the
// inferred provider plus every parameter read from the line. It is what the
// gate validates the provider resource against.
func Directive(inf match.Inference) directive.Directive {
	pairs := []directive.KV{{Key: constant.DirectiveProvider, Value: inf.Provider}}
	if inf.Repository != "" {
		pairs = append(
			pairs,
			directive.KV{Key: constant.DirectiveRepository, Value: inf.Repository},
		)
	}
	if inf.Registry != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveRegistry, Value: inf.Registry})
	}
	if inf.Host != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveHost, Value: inf.Host})
	}
	if inf.Package != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectivePackage, Value: inf.Package})
	}
	if inf.Product != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveProduct, Value: inf.Product})
	}
	if inf.Source != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveSource, Value: inf.Source})
	}
	if inf.TagPrefix != "" {
		pairs = append(pairs, directive.KV{Key: constant.RuleTagPrefix, Value: inf.TagPrefix})
	}
	if inf.Track != "" {
		pairs = append(pairs, directive.KV{Key: constant.DirectiveTrack, Value: inf.Track})
	}
	return directive.Directive{Pairs: pairs}
}
