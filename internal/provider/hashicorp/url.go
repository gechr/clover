package hashicorp

import (
	"net/url"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate: the product's release page
// for the candidate's version. Empty when the version is unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	if !ok || c.Version == "" {
		return ""
	}
	link, err := url.JoinPath("https://"+host, res.product, c.Version)
	if err != nil {
		return ""
	}
	return link
}
