package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

// pageSize bounds tag discovery to one page. The Hub API orders newest-first, so
// its page is the most recent tags; an OCI registry orders lexically, so a
// repository with more than this many tags may have its newest beyond the page.
// Deep history (following the registry's Link pagination) is a future refinement.
const pageSize = 100

// Discover lists candidate versions for a resource from its registry tags,
// routing Docker Hub to its richer API and every other registry to the OCI
// registry v2 tags endpoint.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	ref, ok := r.(reference)
	if !ok {
		return nil, fmt.Errorf("docker: invalid resource %T", r)
	}
	if ref.dockerHub {
		return p.discoverHub(ctx, ref)
	}
	return p.discoverRegistry(ctx, ref)
}

// candidate builds a model.Candidate, parsing the raw tag for comparison. A
// recognized variant suffix (1.27-alpine) is stripped before parsing so the tag
// orders by its numeric core rather than as a prerelease, while a true
// prerelease (2.0.0-rc.1) is kept. A tag that is not semver-shaped yields a nil
// Semver and is skipped by selection.
func candidate(raw string, published time.Time) model.Candidate {
	base, _ := version.SplitVariant(raw)
	semver, _ := version.Parse(base)
	return model.Candidate{
		Version:     raw,
		Semver:      semver,
		Ref:         raw,
		PublishedAt: published,
	}
}
