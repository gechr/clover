package python

import (
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// metaSlug is the [model.Candidate.Meta] key holding a release's python.org
// slug, the one piece the release-page URL needs that the version alone cannot
// reconstruct.
const metaSlug = "slug"

// URL builds the release page for a resolved candidate from its python.org slug
// (e.g. python-3146 -> .../downloads/release/python-3146/). It is empty when the
// slug is absent - the synthesized current-version candidate carries no Meta, so
// only a discovered candidate links.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	slug := c.Meta[metaSlug]
	if _, ok := r.(resource); !ok || slug == "" {
		return ""
	}
	return releasePath + slug + "/"
}
