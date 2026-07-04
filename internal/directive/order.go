package directive

import (
	"cmp"
	"slices"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/x/set"
)

// canonicalLeading, canonicalLocator, and canonicalTrailing are the fixed zones
// of a directive's canonical key order; a provider's own keys slot between the
// leading zone and the locator. The leading zone names the marker and its follow
// edges (provider first, so which provider keys are valid is known); the locator
// zone names the line the directive targets (target for an inline comment,
// jq/find for a sidecar entry) and the region within it; the trailing zone is
// the selection rule then control. Keys not listed here (deferred or unknown) are
// kept, in their original order, after every known key - the grammar warns on
// unknown keys but never drops them, so a future key survives a format pass on
// an older binary.
var (
	canonicalLeading = []string{
		constant.DirectiveProvider,
		constant.DirectiveTrack,
		constant.DirectiveID,
		constant.DirectiveFrom,
		constant.DirectiveValue,
		constant.DirectiveSelect,
	}
	canonicalLocator = []string{
		constant.DirectiveOffset,
		constant.DirectiveTarget,
		constant.DirectiveJQ,
		constant.DirectiveFind,
		constant.DirectiveReplace,
	}
	canonicalTrailing = []string{
		constant.RuleConstraint,
		constant.RuleTagPrefix,
		constant.RuleInclude,
		constant.RuleExclude,
		constant.RuleAsset,
		constant.RulePrerelease,
		constant.RuleCooldown,
		constant.RuleBehind,
		constant.RuleDowngrade,
		constant.DirectiveTags,
		constant.DirectiveDisabled,
	}
)

// repeatableKeys are the multi-valued keys a directive may carry more than once
// (the keys [Directive.All] reads): the text codec repeats the pair, the YAML
// codec collapses them into a single sequence value.
var repeatableKeys = set.New(constant.RuleInclude, constant.RuleExclude)

// csvKeys are the keys whose single value is a comma-separated list (the keys
// [Directive.CSV] reads). The YAML codec lets such a key be written as a
// sequence and joins it back into one comma-separated value, so an item that
// itself holds commas flattens transparently.
var csvKeys = set.New(constant.DirectiveTags)

// Reorder returns the directive with its pairs sorted into canonical order: the
// leading common keys, then providerKeys (the provider's own, in the order it
// declares them), then the locator keys, then the trailing common keys.
// Repeated keys (include, exclude) keep their relative order, and any key in
// none of the zones is kept after the known keys in its original position, so
// nothing is ever dropped.
func Reorder(d Directive, providerKeys []string) Directive {
	order := make(
		[]string,
		0,
		len(canonicalLeading)+len(providerKeys)+len(canonicalLocator)+len(canonicalTrailing),
	)
	order = append(order, canonicalLeading...)
	order = append(order, providerKeys...)
	order = append(order, canonicalLocator...)
	order = append(order, canonicalTrailing...)

	rank := make(map[string]int, len(order))
	for i, key := range order {
		if _, seen := rank[key]; !seen {
			rank[key] = i
		}
	}

	pairs := make([]KV, len(d.Pairs))
	copy(pairs, d.Pairs)
	slices.SortStableFunc(pairs, func(a, b KV) int {
		ra, oka := rank[a.Key]
		rb, okb := rank[b.Key]
		switch {
		case oka && okb:
			return cmp.Compare(ra, rb)
		case oka != okb:
			if oka { // a known key sorts before an unknown one
				return -1
			}
			return 1
		default:
			return 0 // both unknown: stable sort keeps the original order
		}
	})
	return Directive{Pairs: pairs}
}

// isRepeatable reports whether key may appear more than once in a directive.
// Both codecs consult it: the text codec repeats the pair, the YAML codec
// represents the values as a single sequence.
func isRepeatable(key string) bool {
	return repeatableKeys.Contains(key)
}

// isCSV reports whether key's single value is a comma-separated list, so the
// YAML codec may accept a sequence and join it into one value.
func isCSV(key string) bool {
	return csvKeys.Contains(key)
}
