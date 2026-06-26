package helm

import (
	"context"
	"fmt"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

// Discover lists candidate versions for a chart, reading a classic repository's
// index.yaml or an OCI registry's tags.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	ref, ok := r.(reference)
	if !ok {
		return nil, fmt.Errorf("helm: invalid resource %T", r)
	}
	if ref.isOCI {
		return p.discoverRegistry(ctx, ref)
	}
	return p.discoverIndex(ctx, ref)
}

// discoverRegistry lists a chart's versions from an OCI registry's tags via the
// shared client, noting truncation when a shallow lookup left a further page
// unread so the edge can suggest --deep.
func (p *Provider) discoverRegistry(ctx context.Context, ref reference) ([]model.Candidate, error) {
	tags, truncated, err := p.client.Tags(ctx, ref.repo, provider.Deep(ctx))
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

// candidate builds a model.Candidate from a chart version, parsing it for
// comparison. A version that is not semver-shaped yields a nil Semver and is
// skipped by selection.
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
