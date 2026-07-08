package golang

import (
	"cmp"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate: the release's anchor on the
// go.dev downloads page. It links via the upstream ref, which keeps the "go"
// prefix the anchor uses - the current-version candidate carries the bare on-line
// value in Version but the prefixed form in Ref. Empty when the version is
// unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	ref := cmp.Or(c.Ref, c.Version)
	if _, ok := r.(resource); !ok || ref == "" {
		return ""
	}
	return downloadBase + "#" + ref
}
