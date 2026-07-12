package python

import (
	"cmp"
	"strings"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// metaSlug is the [model.Candidate.Meta] key holding a release's python.org
// slug, which the release-page URL is built from.
const metaSlug = "slug"

// URL builds the release page for a resolved candidate from its python.org slug
// (e.g. python-3146 -> .../downloads/release/python-3146/). A candidate without
// one - the synthesized current-version candidate carries no Meta - falls back
// to the slug derived from its version, so the from side links too.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	if _, ok := r.(resource); !ok {
		return ""
	}
	slug := cmp.Or(c.Meta[metaSlug], deriveSlug(cmp.Or(c.Ref, c.Version)))
	if slug == "" {
		return ""
	}
	return releasePath + slug + "/"
}

// slugSeparators removes the version separators a python.org slug drops.
var slugSeparators = strings.NewReplacer(".", "", "-", "")

// deriveSlug reconstructs a release-page slug from a version: python- plus the
// version with its separators removed (3.15.0b3 -> python-3150b3). One historic
// slug deviates (3.3.5rc1 is python-335-rc1), so a discovered candidate's real
// slug always wins over derivation.
func deriveSlug(v string) string {
	if v == "" {
		return ""
	}
	return "python-" + slugSeparators.Replace(v)
}
