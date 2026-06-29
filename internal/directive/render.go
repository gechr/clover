package directive

import (
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/regexlit"
	xslices "github.com/gechr/x/slices"
	xstrings "github.com/gechr/x/strings"
)

// CanonicalizeTags returns d with its tags value lowercased and de-duplicated,
// preserving order. Tag matching is case-insensitive, so this is the canonical
// form format settles a tags value into; an absent tags key is unchanged.
func CanonicalizeTags(d Directive) Directive {
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
		b.WriteRune(constant.DirectiveSeparator)
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
