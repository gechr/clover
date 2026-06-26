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
// client, noting truncation when a shallow lookup left a further page unread so
// the edge can suggest --deep.
func (p *Provider) discoverRegistry(ctx context.Context, ref reference) ([]model.Candidate, error) {
	tags, truncated, err := p.client.Tags(ctx, ref.ociRepo(), provider.Deep(ctx))
	if err != nil {
		return nil, err
	}
	if truncated {
		provider.NoteTruncated(ctx, ref.String(), ref.url())
	}
	candidates := make([]model.Candidate, 0, len(tags))
	for _, t := range tags {
		candidates = append(candidates, candidate(t, time.Time{}))
	}
	return candidates, nil
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
