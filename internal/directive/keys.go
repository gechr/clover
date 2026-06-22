package directive

import (
	"errors"
	"fmt"
	"slices"

	"github.com/gechr/clover/internal/constant"
)

// ErrUnknownKey is the sentinel a [Directive.CheckKeys] failure wraps, so a
// caller can tell an unknown-key rejection apart from other validation errors
// and treat it accordingly - lint rejects it, run downgrades it to a skip.
var ErrUnknownKey = errors.New("unknown key")

// commonKeys is the provider-agnostic directive vocabulary: every key valid on
// any marker regardless of provider. Provider-specific keys (repository, chart,
// registry, platform, source) are not here - they come from the resolved
// provider's own Keys(), so the validator unions the two. A key in neither set
// is unknown: a typo or a stale annotation from another tool, which the grammar
// rejects rather than silently carrying as inert configuration.
var commonKeys = map[string]bool{
	constant.DirectiveProvider:     true,
	constant.DirectiveTrack:        true,
	constant.DirectiveID:           true,
	constant.DirectiveFrom:         true,
	constant.DirectiveValue:        true,
	constant.DirectiveSelect:       true,
	constant.DirectiveFind:         true,
	constant.DirectiveReplace:      true,
	constant.DirectivePattern:      true,
	constant.DirectiveVerify:       true,
	constant.DirectiveVerifyBranch: true,
	constant.DirectiveSha256Source: true,
	constant.DirectiveSha256URL:    true,
	constant.DirectiveSkip:         true,
	constant.DirectiveTags:         true,
	constant.RuleConstraint:        true,
	constant.RuleTagPrefix:         true,
	constant.RuleInclude:           true,
	constant.RuleExclude:           true,
	constant.RuleAsset:             true,
	constant.RulePrerelease:        true,
	constant.RuleCooldown:          true,
	constant.RuleBehind:            true,
	constant.RuleDowngrade:         true,
}

// CheckKeys returns an error naming the first key outside the common vocabulary
// and providerKeys (the resolved provider's own keys), with the closest known
// key suggested when one is near. It returns nil when every key is known. Both
// lint/run validation and format share it, so the rejection reads identically
// wherever a stale or mistyped key surfaces.
func (d Directive) CheckKeys(providerKeys []string) error {
	key, suggestion, found := d.FirstUnknownKey(providerKeys)
	switch {
	case !found:
		return nil
	case suggestion != "":
		return fmt.Errorf("%w %q (did you mean %q?)", ErrUnknownKey, key, suggestion)
	default:
		return fmt.Errorf("%w %q", ErrUnknownKey, key)
	}
}

// PruneUnknownKeys returns the directive with every key outside the common
// vocabulary and providerKeys removed, plus the removed keys in written order.
// The directive is returned unchanged (and removed is nil) when all keys are
// known, so a caller can tell a prune happened. It backs `format --prune`,
// stripping stale annotations a default format would instead reject.
func (d Directive) PruneUnknownKeys(providerKeys []string) (Directive, []string) {
	var removed []string
	for _, kv := range d.Pairs {
		if !commonKeys[kv.Key] && !slices.Contains(providerKeys, kv.Key) {
			removed = append(removed, kv.Key)
		}
	}
	if len(removed) == 0 {
		return d, nil
	}
	kept := make([]KV, 0, len(d.Pairs)-len(removed))
	for _, kv := range d.Pairs {
		if commonKeys[kv.Key] || slices.Contains(providerKeys, kv.Key) {
			kept = append(kept, kv)
		}
	}
	return Directive{Pairs: kept}, removed
}

// FirstUnknownKey returns the first directive key that is neither in the common
// vocabulary nor among providerKeys (the resolved provider's own keys), together
// with the closest known key as a suggestion (empty when none is near enough)
// and found true. When every key is known it returns "", "", false. Keys are
// checked in written order so the reported one is stable.
func (d Directive) FirstUnknownKey(providerKeys []string) (string, string, bool) {
	for _, kv := range d.Pairs {
		if commonKeys[kv.Key] || slices.Contains(providerKeys, kv.Key) {
			continue
		}
		return kv.Key, closest(kv.Key, providerKeys), true
	}
	return "", "", false
}

// suggestionMaxDistanceDivisor bounds how far a known key may sit from an
// unknown one to be offered as a typo suggestion: at most a key-length fraction
// of edits (one third), so a long key tolerates more than a short one. A near
// miss like constriant→constraint is suggested; an unrelated stale key is not.
const suggestionMaxDistanceDivisor = 3

// closest returns the known key nearest to unknown by Levenshtein distance, or
// "" when the nearest is too far to be a plausible typo. It considers the common
// vocabulary plus the provider's keys, so a mistyped repository on docker
// suggests "repository".
func closest(unknown string, providerKeys []string) string {
	best, bestDist := "", 0
	consider := func(candidate string) {
		d := levenshtein(unknown, candidate)
		if best == "" || d < bestDist {
			best, bestDist = candidate, d
		}
	}
	for k := range commonKeys {
		consider(k)
	}
	for _, k := range providerKeys {
		consider(k)
	}
	if bestDist > max(len(unknown)/suggestionMaxDistanceDivisor, 1) {
		return ""
	}
	return best
}

// levenshtein is the edit distance between a and b, the minimum single-character
// insertions, deletions, or substitutions to turn one into the other. It backs
// the typo suggestion and so needs only correctness, not speed.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev = curr
	}
	return prev[len(b)]
}
