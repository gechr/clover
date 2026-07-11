package npm

import (
	"cmp"
	"net/url"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate: the version's page on
// npmjs.com. npm versions are bare semver, so the current-version candidate's
// Version and Ref agree; the ref is still preferred for symmetry with the
// providers whose upstream form carries a prefix. A scoped name keeps its
// literal slash, which the web path expects. Empty when the version is unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	ref := cmp.Or(c.Ref, c.Version)
	if !ok || ref == "" {
		return ""
	}
	link, err := url.JoinPath("https://www."+host, "package", res.pkg, "v", ref)
	if err != nil {
		return ""
	}
	return link
}
