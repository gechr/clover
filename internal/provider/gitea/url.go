package gitea

import (
	"net/url"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// webURL is the repository's web root, e.g. https://codeberg.org/owner/name. It
// roots both the Linker page and the truncation hint.
func webURL(res resource) string {
	link, err := url.JoinPath("https://"+res.host, res.owner, res.name)
	if err != nil {
		return "https://" + res.host
	}
	return link
}

// URL builds the web page for a resolved candidate: the source-at-tag page for
// the candidate's ref, which exists for every tag whether or not it has a release.
// Empty when the ref is unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	if !ok || c.Ref == "" {
		return ""
	}
	link, err := url.JoinPath(webURL(res), "src", "tag", c.Ref)
	if err != nil {
		return ""
	}
	return link
}
