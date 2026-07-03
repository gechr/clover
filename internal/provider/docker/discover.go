package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// pageSize bounds tag discovery to one page. The Hub API orders newest-first, so
// its page is the most recent tags; the OCI registry path orders lexically and
// follows pagination under --deep (see [oci.Client.Tags]).
const pageSize = 100

// Discover lists candidate versions for a resource from its registry tags,
// routing Docker Hub to its richer API and every other registry to the shared
// OCI registry v2 tags endpoint.
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

// discoverRegistry lists tags from a (non-Hub) OCI registry via the shared
// client.
func (p *Provider) discoverRegistry(ctx context.Context, ref reference) ([]model.Candidate, error) {
	return provider.DiscoverOCITags(ctx, p.client, ref.ociRepo(), ref.String(), ref.url())
}

// candidate builds a model.Candidate, parsing the raw tag for comparison; see
// [model.NewVariantCandidate]. A tag that is not semver-shaped yields a nil
// Semver and is skipped by selection.
func candidate(raw string, published time.Time) model.Candidate {
	c := model.NewVariantCandidate(raw)
	c.PublishedAt = published
	return c
}
