package crates

import (
	"cmp"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the crate page for a resolved candidate, versioned when the
// candidate carries a version. The path uses the candidate's Ref - the raw
// form the registry publishes - falling back to Version for a synthesized
// candidate with no ref.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	if !ok {
		return ""
	}
	ref := cmp.Or(c.Ref, c.Version)
	if ref == "" {
		return ""
	}
	return cratePath + res.name + "/" + ref
}
