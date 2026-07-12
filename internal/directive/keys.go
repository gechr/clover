package directive

import (
	"errors"
	"fmt"
	"slices"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/x/set"
	xslices "github.com/gechr/x/slices"
	xstrings "github.com/gechr/x/strings"
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
var commonKeys = set.New(
	constant.DirectiveDisabled,
	constant.DirectiveFind,
	constant.DirectiveFrom,
	constant.DirectiveID,
	constant.DirectiveOffset,
	constant.DirectivePattern,
	constant.DirectiveProvider,
	constant.DirectiveReplace,
	constant.DirectiveSelect,
	constant.DirectiveSha256Source,
	constant.DirectiveSha256URL,
	constant.DirectiveTags,
	constant.DirectiveTarget,
	constant.DirectiveTrack,
	constant.DirectiveValue,
	constant.DirectiveVerify,
	constant.DirectiveVerifyBranch,
	constant.DirectiveVerifySigned,
	constant.RuleAsset,
	constant.RuleBehind,
	constant.RuleConstraint,
	constant.RuleCooldown,
	constant.RuleDowngrade,
	constant.RuleExclude,
	constant.RuleInclude,
	constant.RulePrerelease,
	constant.RuleTagPrefix,
)

// CommonKeys returns the provider-agnostic directive vocabulary in sorted order -
// every key valid on any marker regardless of provider. It exposes the same set
// [CheckKeys] validates against, so a caller (the sidecar schema's drift guard)
// can enumerate the vocabulary without reaching into the unexported map.
func CommonKeys() []string {
	keys := commonKeys.Slice()
	xslices.SortNatural(keys)
	return keys
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

// inlineOnlyKeys anchor a directive relative to its comment line, which a
// sidecar entry (located by its own jq/find) has no use for.
var inlineOnlyKeys = []string{constant.DirectiveOffset, constant.DirectiveTarget}

// CheckKeysSidecar validates a sidecar entry's keys, permitting jq in addition
// to the common vocabulary and providerKeys. jq is a locator key that only a
// sidecar may carry (an inline directive still rejects it via [Directive.CheckKeys]),
// and the comment-relative anchors offset and target are the inverse - inline
// only.
func (d Directive) CheckKeysSidecar(providerKeys []string) error {
	for _, key := range inlineOnlyKeys {
		if d.Has(key) {
			return fmt.Errorf(
				"%q is only valid in an inline directive - a sidecar entry locates its line with %q or %q",
				key,
				constant.DirectiveFind,
				constant.DirectiveJQ,
			)
		}
	}
	return d.CheckKeys(append(slices.Clone(providerKeys), constant.DirectiveJQ))
}

// PruneUnknownKeys returns the directive with every key outside the common
// vocabulary and providerKeys removed, plus the removed keys in written order.
// The directive is returned unchanged (and removed is nil) when all keys are
// known, so a caller can tell a prune happened. It backs `format --prune`,
// stripping stale annotations a default format would instead reject.
func (d Directive) PruneUnknownKeys(providerKeys []string) (Directive, []string) {
	var removed []string
	for _, kv := range d.Pairs {
		if !commonKeys.Contains(kv.Key) && !slices.Contains(providerKeys, kv.Key) {
			removed = append(removed, kv.Key)
		}
	}
	if len(removed) == 0 {
		return d, nil
	}
	kept := make([]KV, 0, len(d.Pairs)-len(removed))
	for _, kv := range d.Pairs {
		if commonKeys.Contains(kv.Key) || slices.Contains(providerKeys, kv.Key) {
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
		if commonKeys.Contains(kv.Key) || slices.Contains(providerKeys, kv.Key) {
			continue
		}
		return kv.Key, closest(kv.Key, providerKeys), true
	}
	return "", "", false
}

// closest returns the known key nearest to unknown as a typo suggestion, or ""
// when none is near enough. It considers the common vocabulary plus the
// provider's keys, so a mistyped repository on docker suggests "repository".
// Candidates are sorted so an equidistant tie resolves deterministically.
func closest(unknown string, providerKeys []string) string {
	candidates := append(commonKeys.Slice(), providerKeys...)
	xslices.SortNatural(candidates)
	return xstrings.Closest(unknown, candidates)
}
