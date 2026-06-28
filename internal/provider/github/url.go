package github

import (
	"net/url"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate: the release/tag page for the
// candidate's ref. The releases/tag form renders for a plain tag too, so it
// serves both source=tags and source=releases. Empty when the ref is unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	if !ok || c.Ref == "" {
		return ""
	}
	link, err := url.JoinPath("https://"+res.host, res.owner, res.name, "releases", "tag", c.Ref)
	if err != nil {
		return ""
	}
	return link
}
