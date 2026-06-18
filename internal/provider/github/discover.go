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

// perPage is the page size. A shallow lookup reads only the first page of
// newest entries - enough for selection and respectful of the rate limit - so a
// tag marker costs one request (releases costs two, also fetching tags to
// resolve commits). A deep lookup pages to exhaustion instead; see listAll.
const perPage = 100

// tag is the subset of the /tags response clover reads.
type tag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// release is the subset of the /releases response clover reads. Each asset
// carries a content digest the API computes, so a follower can source a sha256
// without a download.
type release struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Assets      []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
		URL    string `json:"browser_download_url"`
	} `json:"assets"`
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
		candidates = append(candidates, candidate(t.Name, t.Commit.SHA, time.Time{}, nil))
	}
	return candidates, nil
}

func discoverReleases(
	ctx context.Context,
	rest *api.RESTClient,
	res resource,
) ([]model.Candidate, error) {
	releases, err := listAll[release](ctx, rest, "releases", func(page int) string {
		return fmt.Sprintf(
			"repos/%s/%s/releases?per_page=%d&page=%d",
			res.owner,
			res.name,
			perPage,
			page,
		)
	})
	if err != nil {
		return nil, err
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
		assets := make([]model.Asset, 0, len(rel.Assets))
		for _, a := range rel.Assets {
			assets = append(assets, model.Asset{Name: a.Name, Digest: a.Digest, URL: a.URL})
		}
		candidates = append(
			candidates,
			candidate(rel.TagName, commits[rel.TagName], rel.PublishedAt, assets),
		)
	}
	return candidates, nil
}

// listTags returns the newest page of a repository's tags, or every page when
// ctx requests a deep lookup.
func listTags(ctx context.Context, rest *api.RESTClient, res resource) ([]tag, error) {
	return listAll[tag](ctx, rest, "tags", func(page int) string {
		return fmt.Sprintf(
			"repos/%s/%s/tags?per_page=%d&page=%d",
			res.owner,
			res.name,
			perPage,
			page,
		)
	})
}

// listAll fetches the first page of pathFor(page), or every page when ctx
// requests a deep lookup, stopping at the first short page (the last one). The
// shallow default is one request of the newest perPage entries, which suffices
// for these recency-ordered listings; deep lookup trades requests for
// completeness across a repository's whole history.
func listAll[T any](
	ctx context.Context,
	rest *api.RESTClient,
	what string,
	pathFor func(page int) string,
) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		var batch []T
		if err := rest.DoWithContext(ctx, http.MethodGet, pathFor(page), nil, &batch); err != nil {
			return nil, fmt.Errorf("github: list %s: %w", what, err)
		}
		all = append(all, batch...)
		if !provider.Deep(ctx) || len(batch) < perPage {
			break
		}
	}
	return all, nil
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
func candidate(raw, commit string, published time.Time, assets []model.Asset) model.Candidate {
	semver, _ := version.Parse(raw)
	return model.Candidate{
		Version:     raw,
		Semver:      semver,
		Commit:      commit,
		Ref:         raw,
		PublishedAt: published,
		Assets:      assets,
	}
}
