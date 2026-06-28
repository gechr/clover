package gitlab

import (
	"net/url"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// webURL is the project's web root, e.g. https://gitlab.com/group/project. It
// roots both the Linker page and the truncation hint.
func webURL(res resource) string {
	link, err := url.JoinPath("https://"+host, res.repository)
	if err != nil {
		return "https://" + host
	}
	return link
}

// URL builds the web page for a resolved candidate: the tag page for the
// candidate's ref. Every release has a tag, so the tag page serves both
// source=tags and source=releases. Empty when the ref is unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	if !ok || c.Ref == "" {
		return ""
	}
	link, err := url.JoinPath(webURL(res), "-", "tags", c.Ref)
	if err != nil {
		return ""
	}
	return link
}
