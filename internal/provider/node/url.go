package node

import (
	"cmp"
	"net/url"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate: the version's artifact
// directory on nodejs.org. It links via the upstream ref, which keeps the
// v-prefix the dist path requires - the current-version candidate carries the
// bare on-line value in Version but the v-prefixed form in Ref. Empty when the
// version is unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	ref := cmp.Or(c.Ref, c.Version)
	if _, ok := r.(resource); !ok || ref == "" {
		return ""
	}
	link, err := url.JoinPath("https://"+host, "dist", ref)
	if err != nil {
		return ""
	}
	return link + "/"
}
