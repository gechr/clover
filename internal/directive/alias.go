package directive

import "github.com/gechr/clover/internal/constant"

// keyAliases maps a non-canonical directive KEY spelling to its canonical form.
// An alias is accepted wherever a directive is interpreted and is rewritten to
// the canonical spelling by format; the vocabulary itself stays canonical-only,
// so an alias never needs its own entry in the key validator or the schema.
var keyAliases = map[string]string{
	"flavour": constant.DirectiveFlavor,
}

// valueAliases maps a canonical KEY to the alias->canonical rewrites permitted
// for that key's VALUE. A value alias fires only under its exact key, never
// globally: "golang" becomes "go" under provider=, but a "golang" value under
// any other key is left untouched.
var valueAliases = map[string]map[string]string{
	constant.DirectiveProvider: {
		"golang": constant.ProviderGo,
	},
}

// CanonicalizeAliases rewrites any aliased key, and any key-scoped aliased
// value, in d to its canonical form, preserving pair order and leaving every
// other pair verbatim. It is the single alias-rewrite point: run and lint apply
// it so an alias is accepted, and format applies it so an alias is normalized in
// place. A pair's key is canonicalized first, so a value alias is looked up
// under the already-canonical key.
func CanonicalizeAliases(d Directive) Directive {
	pairs := make([]KV, len(d.Pairs))
	copy(pairs, d.Pairs)
	for i := range pairs {
		if canon, ok := keyAliases[pairs[i].Key]; ok {
			pairs[i].Key = canon
		}
		if values, ok := valueAliases[pairs[i].Key]; ok {
			if canon, ok := values[pairs[i].Value]; ok {
				pairs[i].Value = canon
			}
		}
	}
	return Directive{Pairs: pairs}
}
