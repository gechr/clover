package terraform

import (
	"cmp"
	"fmt"
	"strings"

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
	if res.module() {
		return fmt.Sprintf(p.registry.module, res.namespace, res.name, res.target, ref)
	}
	return fmt.Sprintf(p.registry.web, res.namespace, res.name, ref)
}

// Identify returns the source address and, on a public registry, its web page.
// A private registry (an explicit host) has no known web UI, so the URL goes
// empty. The landing page is the version page's format with the trailing
// version segment dropped.
func (p *Provider) Identify(r provider.Resource) (string, string) {
	res, ok := r.(resource)
	if !ok {
		return "", ""
	}
	id := res.address()
	if res.host != p.registry.host {
		return id, ""
	}
	if res.module() {
		return id, fmt.Sprintf(landing(p.registry.module), res.namespace, res.name, res.target)
	}
	return id, fmt.Sprintf(landing(p.registry.web), res.namespace, res.name)
}

// landing drops a version page format's trailing version segment, leaving the
// resource's own page.
func landing(page string) string {
	if before, _, ok := strings.CutLast(page, "/"); ok {
		return before
	}
	return page
}
