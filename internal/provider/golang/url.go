package golang

import (
	"cmp"
	"strings"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate: the release's anchor on the
// go.dev downloads page. The anchor wears the "go" prefix, which only a
// discovered candidate's Ref carries - the synthesized current-version candidate
// arrives bare, since the pipeline reconstructs only a v-style prefix - so the
// prefix is re-applied here whenever it is missing. Empty when the version is
// unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	ref := cmp.Or(c.Ref, c.Version)
	if _, ok := r.(resource); !ok || ref == "" {
		return ""
	}
	if !strings.HasPrefix(ref, versionPrefix) {
		ref = versionPrefix + ref
	}
	return downloadBase + "#" + ref
}
