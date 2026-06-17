package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

// perPage is the page size; the first page of newest entries is enough for
// selection and bounds each marker to one request for tags (two for releases,
// which also fetch tags to resolve commits), which respects the rate limit.
// Deep history is a future refinement.
const perPage = 100

// tag is the subset of the /tags response clover reads.
type tag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// release is the subset of the /releases response clover reads.
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
	tags, err := listTags(ctx, rest, res)
	if err != nil {
		return nil, err
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

	// The releases payload carries no commit SHA, but action-pinning needs one.
	// The tags list returns each tag's commit already peeled (annotated tags
	// dereferenced to their target commit), so resolve commits by joining on
	// tag name - one extra request, not one per release. A release whose tag is
	// older than the newest perPage tags resolves to an empty commit, which only
	// blocks action-pinning that single marker.
	commits, err := tagCommits(ctx, rest, res)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		if rel.Draft {
			continue
		}
		candidates = append(
			candidates,
			candidate(rel.TagName, commits[rel.TagName], rel.PublishedAt),
		)
	}
	return candidates, nil
}

// listTags returns the newest page of a repository's tags.
func listTags(ctx context.Context, rest *api.RESTClient, res resource) ([]tag, error) {
	var tags []tag
	path := fmt.Sprintf("repos/%s/%s/tags?per_page=%d", res.owner, res.name, perPage)
	if err := rest.DoWithContext(ctx, http.MethodGet, path, nil, &tags); err != nil {
		return nil, fmt.Errorf("github: list tags: %w", err)
	}
	return tags, nil
}

// tagCommits maps tag name to its peeled commit SHA, for resolving the commit a
// release points at.
func tagCommits(
	ctx context.Context,
	rest *api.RESTClient,
	res resource,
) (map[string]string, error) {
	tags, err := listTags(ctx, rest, res)
	if err != nil {
		return nil, err
	}
	commits := make(map[string]string, len(tags))
	for _, t := range tags {
		commits[t.Name] = t.Commit.SHA
	}
	return commits, nil
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
