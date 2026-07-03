package gitea

import (
	"context"
	"fmt"
	"time"

	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	xhttp "github.com/gechr/x/http"
)

// perPage is the page size, Gitea's ceiling for the list endpoints. A shallow
// lookup reads only the first page; a deep lookup follows the Link header to
// exhaustion.
const perPage = 50

// tag is the subset of the /tags response clover reads. Gitea reports no creation
// time for a tag - only the target commit's date - so PublishedAt is left zero
// rather than aged by a commit that may predate the tag by months; cooldown is
// then inert for a tag, matching the github provider.
type tag struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// release is the subset of the /releases response clover reads. Draft releases
// are unpublished and not candidates; Prerelease flags a release the upstream
// marks pre-release out of band of its tag, which selection must honor even when
// the tag itself looks stable. PublishedAt is the release's own publication time
// (cooldown's basis). target_commitish is deliberately not read: it may be empty
// or a branch name, not a commit SHA, so it must not feed Candidate.Commit.
// Assets carry no content digest, so a follower cannot source a sha256 without a
// download.
type release struct {
	TagName     string            `json:"tag_name"`
	Draft       bool              `json:"draft"`
	Prerelease  bool              `json:"prerelease"`
	PublishedAt dates.ReleaseTime `json:"published_at"`
	Assets      []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Discover lists candidate versions for a resource from tags or releases.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("gitea: invalid resource %T", r)
	}

	switch res.source {
	case forge.SourceReleases:
		return p.discoverReleases(ctx, res)
	case forge.SourceTags:
		return p.discoverTags(ctx, res)
	}
	return nil, fmt.Errorf("gitea: unknown source %q", res.source)
}

// discoverTags lists a project's tags as candidates. Gitea orders tags by
// creation date, not version, and offers no version-sort parameter, so the first
// page is not guaranteed to hold the highest version - which is why the provider
// does not implement RecencyOrderer and keeps the blanket --deep hint.
func (p *Provider) discoverTags(ctx context.Context, res resource) ([]model.Candidate, error) {
	start := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/tags?limit=%d", res.owner, res.name, perPage),
	)
	tags, truncated, err := listAll[tag](ctx, p.rest, "tags", start, p.authorization(ctx, res.host))
	if err != nil {
		return nil, err
	}
	if truncated {
		provider.NoteTruncated(ctx, p.Describe(res), webURL(res)+"/tags")
	}

	candidates := make([]model.Candidate, 0, len(tags))
	for _, t := range tags {
		candidates = append(candidates, candidate(t.Name, t.Commit.SHA, false, time.Time{}, nil))
	}
	return candidates, nil
}

// discoverReleases lists a project's releases as candidates, newest-first by
// publication date.
func (p *Provider) discoverReleases(ctx context.Context, res resource) ([]model.Candidate, error) {
	start := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/releases?limit=%d", res.owner, res.name, perPage),
	)
	releases, truncated, err := listAll[release](
		ctx,
		p.rest,
		"releases",
		start,
		p.authorization(ctx, res.host),
	)
	if err != nil {
		return nil, err
	}
	if truncated {
		provider.NoteTruncated(ctx, p.Describe(res), webURL(res)+"/releases")
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		// A draft release is unpublished, so it is not a candidate - mirroring the
		// github provider.
		if rel.TagName == "" || rel.Draft {
			continue
		}
		assets := make([]model.Asset, 0, len(rel.Assets))
		for _, a := range rel.Assets {
			assets = append(assets, model.Asset{Name: a.Name, URL: a.URL})
		}
		// target_commitish is not a reliable commit SHA (often empty or a branch
		// name), so Commit is left empty for releases rather than misreporting it.
		candidates = append(
			candidates,
			candidate(rel.TagName, "", rel.Prerelease, rel.PublishedAt.Time, assets),
		)
	}
	return candidates, nil
}

// listAll fetches the first page at start, or every page when ctx requests a deep
// lookup, following Gitea's Link header rather than guessing from the item count.
// The second return reports a truncated shallow lookup - a first page with more
// behind it - so a constrained marker that finds no candidate can be hinted toward
// --deep.
func listAll[T any](
	ctx context.Context,
	rest forge.RESTClient,
	what, start, authorization string,
) ([]T, bool, error) {
	var all []T
	for url := start; ; {
		var batch []T
		header, err := rest.DoWithContext(ctx, url, authorization, &batch)
		if err != nil {
			return nil, false, fmt.Errorf("gitea: list %s: %w", what, err)
		}
		all = append(all, batch...)
		next := xhttp.NextLink(header)
		// Never follow a next page to a different origin: the credential must not
		// leak off the host the lookup started on.
		if next != "" && !forge.SameOrigin(start, next) {
			next = ""
		}
		if next == "" || !provider.Deep(ctx) {
			return all, !provider.Deep(ctx) && next != "", nil
		}
		url = next
	}
}

// candidate builds a model.Candidate, parsing the raw tag for comparison and
// carrying the commit SHA, prerelease flag, and date the API supplied for free. A
// tag that is not semver-shaped yields a nil Semver and is skipped by selection.
func candidate(
	raw, commit string,
	prerelease bool,
	published time.Time,
	assets []model.Asset,
) model.Candidate {
	semver, _ := version.Parse(raw)
	return model.Candidate{
		Version:     raw,
		Semver:      semver,
		Prerelease:  prerelease,
		Commit:      commit,
		Ref:         raw,
		PublishedAt: published,
		Assets:      assets,
	}
}
