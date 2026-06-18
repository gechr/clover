package directive

import (
	"sort"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/regexlit"
	xslices "github.com/gechr/x/slices"
	xstrings "github.com/gechr/x/strings"
)

// canonicalLeading and canonicalTrailing are the fixed zones of a directive's
// canonical key order; a provider's own keys slot between them. The leading zone
// names the marker and its follow edges (provider first, so which provider keys
// are valid is known); the trailing zone is the selection rule then control.
// Keys not listed here (deferred or unknown) are kept, in their original order,
// after every known key - the grammar warns on unknown keys but never drops
// them, so a future key survives a format pass on an older binary.
var (
	canonicalLeading = []string{
		constant.DirectiveProvider,
		constant.DirectiveID,
		constant.DirectiveFrom,
		constant.DirectiveValue,
		constant.DirectiveSelect,
	}
	canonicalTrailing = []string{
		constant.RuleConstraint,
		constant.RuleInclude,
		constant.RuleExclude,
		constant.RulePrerelease,
		constant.RuleCooldown,
		constant.RuleBehind,
		constant.RuleAllowDowngrade,
		constant.DirectiveTags,
		constant.DirectiveSkip,
	}
)

// Reorder returns the directive with its pairs sorted into canonical order:
// the leading common keys, then providerKeys (the provider's own, in the order
// it declares them), then the trailing common keys. Repeated keys (include,
// exclude) keep their relative order, and any key in none of the zones is kept
// after the known keys in its original position, so nothing is ever dropped.
func Reorder(d Directive, providerKeys []string) Directive {
	order := make([]string, 0, len(canonicalLeading)+len(providerKeys)+len(canonicalTrailing))
	order = append(order, canonicalLeading...)
	order = append(order, providerKeys...)
	order = append(order, canonicalTrailing...)

	rank := make(map[string]int, len(order))
	for i, key := range order {
		if _, seen := rank[key]; !seen {
			rank[key] = i
		}
	}

	pairs := make([]KV, len(d.Pairs))
	copy(pairs, d.Pairs)
	sort.SliceStable(pairs, func(i, j int) bool {
		ri, oki := rank[pairs[i].Key]
		rj, okj := rank[pairs[j].Key]
		switch {
		case oki && okj:
			return ri < rj
		case oki != okj:
			return oki // a known key sorts before an unknown one
		default:
			return false // both unknown: stable sort keeps the original order
		}
	})
	return Directive{Pairs: pairs}
}

// CanonicaliseTags returns d with its tags value lowercased and de-duplicated,
// preserving order. Tag matching is case-insensitive, so this is the canonical
// form format settles a tags value into; an absent tags key is unchanged.
func CanonicaliseTags(d Directive) Directive {
	pairs := make([]KV, len(d.Pairs))
	copy(pairs, d.Pairs)
	for i := range pairs {
		if pairs[i].Key == constant.DirectiveTags {
			pairs[i].Value = canonicalTags(pairs[i].Value)
		}
	}
	return Directive{Pairs: pairs}
}

// canonicalTags lowercases and de-duplicates a comma-separated tags value,
// trimming each tag and dropping empties, with order preserved.
func canonicalTags(value string) string {
	tags := xstrings.SplitCSV(value)
	for i := range tags {
		tags[i] = strings.ToLower(tags[i])
	}
	return strings.Join(xslices.Unique(tags), ",")
}

// Render serializes a directive to its canonical text: the keyword, then each
// pair as key=value separated by single spaces. A value is quoted only when it
// must be - when it contains whitespace, or when its first character (a quote or
// a slash) would otherwise re-trigger quoted or /regex/ parsing - and a value
// that is already a complete /regex/ is left bare because it self-delimits. This
// makes Render the exact inverse of Parse, so format is idempotent.
func Render(d Directive) string {
	var b strings.Builder
	b.WriteString(constant.DirectiveKeyword)
	for _, kv := range d.Pairs {
		b.WriteByte(' ')
		b.WriteString(kv.Key)
		b.WriteRune(constant.DirectiveEqual)
		b.WriteString(renderValue(kv.Value))
	}
	return b.String()
}

// renderValue quotes v when leaving it bare would not round-trip through Parse.
func renderValue(v string) string {
	if v == "" || regexlit.Is(v) {
		return v
	}
	if v[0] == regexlit.Delim || isQuote(rune(v[0])) || strings.ContainsAny(v, " \t") {
		return quote(v)
	}
	return v
}

// quote wraps v in the first quote character it does not itself contain, so the
// matching quote unambiguously closes the value on the way back in.
func quote(v string) string {
	for _, q := range []rune{'"', '\'', '`'} {
		if !strings.ContainsRune(v, q) {
			return string(q) + v + string(q)
		}
	}
	return `"` + v + `"` // pathological: v holds all three quote characters
}
