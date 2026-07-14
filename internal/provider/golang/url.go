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
		ref = versionPrefix + goAnchor(ref)
	}
	return downloadBase + "#" + ref
}

// goAnchor collapses a canonical semver into go.dev's download-page anchor
// spelling. A discovered candidate's Ref already wears it; only the synthesized
// current-version candidate arrives as canonical semver, whose prerelease form
// (1.27.0-rc1) must lose its dash and zero patch to match go.dev's anchor
// (1.27rc1, becoming go1.27rc1 once the prefix is applied).
func goAnchor(ref string) string {
	base, pre, ok := strings.Cut(ref, "-")
	if !ok {
		return ref
	}
	return strings.TrimSuffix(base, ".0") + pre
}
