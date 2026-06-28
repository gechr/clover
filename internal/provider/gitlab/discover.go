package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

// perPage is the page size, GitLab's ceiling for the list endpoints. A shallow
// lookup reads only the first page - enough for selection, since the listing is
// newest-first - so a marker costs one request. A deep lookup pages to exhaustion.
const perPage = 100

// tag is the subset of the /repository/tags response clover reads. CreatedAt is
// the tag's own creation time (cooldown's basis), not the target commit's date - a
// tag cut today on an old commit must read as new. GitLab returns null for a
// lightweight tag, which decodes to the zero time, leaving cooldown inert rather
// than falsely aged.
type tag struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Commit    struct {
		ID string `json:"id"`
	} `json:"commit"`
}

// release is the subset of the /releases response clover reads. UpcomingRelease
// marks a release dated in the future, which is not yet published and so is not a
// candidate. assets.links are arbitrary URLs GitLab attaches to a release; unlike
// a GitHub asset they carry no content digest, so a follower cannot source a
// sha256 without a download.
type release struct {
	TagName         string    `json:"tag_name"`
	ReleasedAt      time.Time `json:"released_at"`
	UpcomingRelease bool      `json:"upcoming_release"`
	Commit          struct {
		ID string `json:"id"`
	} `json:"commit"`
	Assets struct {
		Links []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"links"`
	} `json:"assets"`
}

// Discover lists candidate versions for a resource from tags or releases.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("gitlab: invalid resource %T", r)
	}

	switch res.source {
	case sourceReleases:
		return p.discoverReleases(ctx, res)
	case sourceTags:
		return p.discoverTags(ctx, res)
	}
	return nil, fmt.Errorf("gitlab: unknown source %q", res.source)
}

// discoverTags lists a project's tags as candidates, ordered highest-version
// first by the order_by=version&sort=desc query, so the shallow first page holds
// the latest version - not the most recently updated tag, which a backport to an
// old release line would otherwise float to the top.
func (p *Provider) discoverTags(ctx context.Context, res resource) ([]model.Candidate, error) {
	tags, truncated, err := listAll[tag](ctx, p.rest, "tags", func(page int) string {
		return fmt.Sprintf(
			"projects/%s/repository/tags?order_by=version&sort=desc&per_page=%d&page=%d",
			res.projectID(),
			perPage,
			page,
		)
	})
	if err != nil {
		return nil, err
	}
	if truncated {
		provider.NoteTruncated(ctx, p.Describe(res), webURL(res)+"/-/tags")
	}

	candidates := make([]model.Candidate, 0, len(tags))
	for _, t := range tags {
		candidates = append(candidates, candidate(t.Name, t.Commit.ID, t.CreatedAt, nil))
	}
	return candidates, nil
}

// discoverReleases lists a project's releases as candidates. The endpoint is
// newest-first by release date.
func (p *Provider) discoverReleases(ctx context.Context, res resource) ([]model.Candidate, error) {
	releases, truncated, err := listAll[release](ctx, p.rest, "releases", func(page int) string {
		return fmt.Sprintf(
			"projects/%s/releases?per_page=%d&page=%d",
			res.projectID(),
			perPage,
			page,
		)
	})
	if err != nil {
		return nil, err
	}
	if truncated {
		provider.NoteTruncated(ctx, p.Describe(res), webURL(res)+"/-/releases")
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		// An upcoming release is scheduled but not yet published, so it is not a
		// candidate - mirroring the github provider dropping draft releases.
		if rel.TagName == "" || rel.UpcomingRelease {
			continue
		}
		assets := make([]model.Asset, 0, len(rel.Assets.Links))
		for _, l := range rel.Assets.Links {
			assets = append(assets, model.Asset{Name: l.Name, URL: l.URL})
		}
		candidates = append(
			candidates,
			candidate(rel.TagName, rel.Commit.ID, rel.ReleasedAt, assets),
		)
	}
	return candidates, nil
}

// listAll fetches the first page of pathFor(page), or every page when ctx
// requests a deep lookup. It learns whether more pages remain from GitLab's
// X-Next-Page header rather than guessing from the item count, so a full final
// page is not mistaken for a truncated one. The second return reports a truncated
// shallow lookup - a first page with more behind it - so a constrained marker that
// finds no candidate can be hinted toward --deep.
func listAll[T any](
	ctx context.Context,
	rest *restClient,
	what string,
	pathFor func(page int) string,
) ([]T, bool, error) {
	var all []T
	for page := 1; ; page++ {
		var batch []T
		header, err := rest.DoWithContext(ctx, http.MethodGet, pathFor(page), nil, &batch)
		if err != nil {
			return nil, false, fmt.Errorf("gitlab: list %s: %w", what, err)
		}
		all = append(all, batch...)
		hasNext := header.Get("X-Next-Page") != ""
		if !hasNext || !provider.Deep(ctx) {
			return all, !provider.Deep(ctx) && hasNext, nil
		}
	}
}

// candidate builds a model.Candidate, parsing the raw tag for comparison and
// carrying the commit SHA and date the API supplied for free. A tag that is not
// semver-shaped yields a nil Semver and is skipped by selection.
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
