package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/gechr/cusp/internal/model"
	"github.com/gechr/cusp/internal/provider"
	"github.com/gechr/cusp/internal/version"
)

// perPage is the page size; the first page of newest entries is enough for
// selection and keeps each marker to a single request, which respects the rate
// limit. Deep history is a future refinement.
const perPage = 100

// tag is the subset of the /tags response cusp reads.
type tag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// release is the subset of the /releases response cusp reads.
type release struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
}

// Discover lists candidate versions for a resource from tags or releases.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("github: invalid resource %T", r)
	}

	rest, err := p.client()
	if err != nil {
		return nil, fmt.Errorf("github: build client: %w", err)
	}

	switch res.source {
	case sourceReleases:
		return discoverReleases(ctx, rest, res)
	case sourceTags:
		return discoverTags(ctx, rest, res)
	}
	return nil, fmt.Errorf("github: unknown source %q", res.source)
}

func discoverTags(
	ctx context.Context,
	rest *api.RESTClient,
	res resource,
) ([]model.Candidate, error) {
	var tags []tag
	path := fmt.Sprintf("repos/%s/%s/tags?per_page=%d", res.owner, res.name, perPage)
	if err := rest.DoWithContext(ctx, http.MethodGet, path, nil, &tags); err != nil {
		return nil, fmt.Errorf("github: list tags: %w", err)
	}

	candidates := make([]model.Candidate, 0, len(tags))
	for _, t := range tags {
		candidates = append(candidates, candidate(t.Name, t.Commit.SHA, time.Time{}))
	}
	return candidates, nil
}

func discoverReleases(
	ctx context.Context,
	rest *api.RESTClient,
	res resource,
) ([]model.Candidate, error) {
	var releases []release
	path := fmt.Sprintf("repos/%s/%s/releases?per_page=%d", res.owner, res.name, perPage)
	if err := rest.DoWithContext(ctx, http.MethodGet, path, nil, &releases); err != nil {
		return nil, fmt.Errorf("github: list releases: %w", err)
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		if rel.Draft {
			continue
		}
		candidates = append(candidates, candidate(rel.TagName, "", rel.PublishedAt))
	}
	return candidates, nil
}

// candidate builds a model.Candidate, parsing the raw tag for comparison. A tag
// that is not semver-shaped yields a nil Semver and is skipped by selection.
func candidate(raw, commit string, published time.Time) model.Candidate {
	semver, _ := version.Parse(raw)
	return model.Candidate{
		Version:     raw,
		Semver:      semver,
		Commit:      commit,
		Ref:         raw,
		PublishedAt: published,
	}
}
