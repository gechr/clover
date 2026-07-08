package zig

import (
	"cmp"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate: the release's file directory
// on ziglang.org (e.g. .../download/0.16.0/). The version is clean semver and is
// also the upstream ref, so the current-version candidate - whose Version is the
// bare on-line value - links correctly with no Meta. Empty when the version is
// unknown.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	ref := cmp.Or(c.Ref, c.Version)
	if _, ok := r.(resource); !ok || ref == "" {
		return ""
	}
	return downloadBase + ref + "/"
}
