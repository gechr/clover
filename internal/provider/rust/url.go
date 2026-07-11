package rust

import (
	"cmp"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the release-notes page for a resolved candidate: the version's tag
// page on the rust-lang/rust repository. Rust versions are bare, so Ref and
// Version agree and the synthesized current-version candidate links too. Only
// stable releases are tagged there - a beta snapshot has no tag, so a
// beta-channel resource is never linked.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	ref := cmp.Or(c.Ref, c.Version)
	res, ok := r.(resource)
	if !ok || ref == "" || res.channel == channelBeta {
		return ""
	}
	return releasePath + ref
}
