package pypi

import (
	"cmp"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the project page for a resolved candidate, versioned when the
// candidate carries a version. The path uses the candidate's Ref - the raw
// PEP 440 form PyPI publishes (0.5.30rc1), which its URLs accept - falling
// back to Version for a synthesized candidate with no ref.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	if !ok {
		return ""
	}
	ref := cmp.Or(c.Ref, c.Version)
	if ref == "" {
		return ""
	}
	return projectPath + res.name + "/" + ref + "/"
}
