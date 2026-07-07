package terraform

import (
	"cmp"
	"fmt"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// URL builds the web page for a resolved candidate on the registry's public
// registry. A private registry (an explicit host) has no known web UI, so the
// link goes empty. Registry versions are bare semver, so Version and Ref
// coincide; Ref is still preferred for the synthesized current-version
// candidate.
func (p *Provider) URL(r provider.Resource, c model.Candidate) string {
	res, ok := r.(resource)
	ref := cmp.Or(c.Ref, c.Version)
	if !ok || ref == "" || res.host != p.registry.host {
		return ""
	}
	return fmt.Sprintf(p.registry.web, res.namespace, res.name, ref)
}
