package github

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

// perPage is the page size, GitHub's ceiling for both the REST tags endpoint and
// a GraphQL refs connection. A shallow lookup reads only the first page - enough
// for selection and respectful of the rate limit - so a tag marker costs one
// request. A deep lookup pages to exhaustion instead.
const perPage = 100

// tagsQuery lists a repository's tags ordered newest-first by their target
// commit date - the order the GitHub web UI shows, which the REST tags endpoint
// cannot produce (it has no sort parameter). An annotated tag's target is a Tag
// object whose own target is the peeled commit; a lightweight tag's target is
// the commit directly. first=100 is GitHub's page ceiling (perPage).
const tagsQuery = `query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    refs(refPrefix: "refs/tags/", first: 100, after: $cursor, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}) {
      nodes {
        name
        target {
          __typename
          oid
          ... on Tag { target { oid } }
        }
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

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
	Prerelease  bool      `json:"prerelease"`
	Assets      []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
		URL    string `json:"browser_download_url"`
	} `json:"assets"`
}

// gqlTarget is a tag ref's GraphQL target, carrying enough to peel an annotated
// tag to its commit.
type gqlTarget struct {
	Typename string `json:"__typename"`
	OID      string `json:"oid"`
	Target   struct {
		OID string `json:"oid"`
	} `json:"target"`
}

// commit peels the target to its commit SHA, dereferencing an annotated tag
// (whose own target is the commit) and reading a lightweight tag's oid directly.
func (t gqlTarget) commit() string {
	if t.Typename == "Tag" {
		return t.Target.OID
	}
	return t.OID
}

// gqlTagsResponse mirrors the `data` shape of tagsQuery.
type gqlTagsResponse struct {
	Repository struct {
		Refs struct {
			Nodes []struct {
				Name   string    `json:"name"`
				Target gqlTarget `json:"target"`
			} `json:"nodes"`
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
		} `json:"refs"`
	} `json:"repository"`
}

// Discover lists candidate versions for a resource from tags or releases.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("github: invalid resource %T", r)
	}

	switch res.source {
	case sourceReleases:
		return p.discoverReleases(ctx, res)
	case sourceTags:
		return p.discoverTags(ctx, res)
	}
	return nil, fmt.Errorf("github: unknown source %q", res.source)
}

// discoverTags lists a repository's tags as candidates. The authenticated
// GraphQL path is ordered newest-first, so its shallow first page already holds
// the latest version. The anonymous REST path is not ordered (the endpoint has
// no sort, and a repo's legacy tag namespace - e.g. golang/go's weekly.* tags -
// can bury every real version past page one), so a full shallow page that yields
// no parsable version escalates to a deep lookup once.
func (p *Provider) discoverTags(ctx context.Context, res resource) ([]model.Candidate, error) {
	tags, err := p.listTags(ctx, res)
	if err != nil {
		return nil, err
	}
	candidates := candidatesFromTags(tags)

	if !p.authenticated() && !provider.Deep(ctx) && len(tags) == perPage &&
		!anyParsable(candidates) {
		tags, err = p.listTags(provider.WithDeep(ctx, true), res)
		if err != nil {
			return nil, err
		}
		candidates = candidatesFromTags(tags)
	}
	return candidates, nil
}

// candidatesFromTags projects discovered tags into candidates.
func candidatesFromTags(tags []tag) []model.Candidate {
	candidates := make([]model.Candidate, 0, len(tags))
	for _, t := range tags {
		candidates = append(candidates, candidate(t.Name, t.Commit.SHA, false, time.Time{}, nil))
	}
	return candidates
}

// anyParsable reports whether any candidate carries a parsable version - the
// signal that a shallow page held at least one real version, so a deep fallback
// would add nothing.
func anyParsable(candidates []model.Candidate) bool {
	for _, c := range candidates {
		if c.Semver != nil {
			return true
		}
	}
	return false
}

func (p *Provider) discoverReleases(ctx context.Context, res resource) ([]model.Candidate, error) {
	releases, err := listAll[release](ctx, p.client(), "releases", func(page int) string {
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
	commits, err := p.tagCommits(ctx, res)
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
			candidate(rel.TagName, commits[rel.TagName], rel.Prerelease, rel.PublishedAt, assets),
		)
	}
	return candidates, nil
}

// listTags returns a repository's tags, newest-first. With a credential it uses
// the GraphQL refs API ordered by tag commit date; anonymously it falls back to
// the REST tags endpoint, since GraphQL rejects unauthenticated requests.
// Shallow reads the first page; a deep lookup pages to exhaustion.
func (p *Provider) listTags(ctx context.Context, res resource) ([]tag, error) {
	if p.authenticated() {
		return p.listTagsGraphQL(ctx, res)
	}
	return p.listTagsREST(ctx, res)
}

// listTagsREST reads the REST tags endpoint. The endpoint has no sort, so its
// order is git's raw ref order - not necessarily newest-first; discoverTags
// guards that with its deep fallback.
func (p *Provider) listTagsREST(ctx context.Context, res resource) ([]tag, error) {
	return listAll[tag](ctx, p.client(), "tags", func(page int) string {
		return fmt.Sprintf(
			"repos/%s/%s/tags?per_page=%d&page=%d",
			res.owner,
			res.name,
			perPage,
			page,
		)
	})
}

// listTagsGraphQL reads tags through the GraphQL refs connection, ordered
// newest-first by target commit date. Shallow returns the first page; a deep
// lookup follows the cursor to exhaustion.
func (p *Provider) listTagsGraphQL(ctx context.Context, res resource) ([]tag, error) {
	gql, err := p.gqlClient()
	if err != nil {
		return nil, fmt.Errorf("github: build client: %w", err)
	}
	var (
		all    []tag
		cursor *string
	)
	for {
		var resp gqlTagsResponse
		variables := map[string]any{"owner": res.owner, "name": res.name, "cursor": cursor}
		if err := gql.DoWithContext(ctx, tagsQuery, variables, &resp); err != nil {
			return nil, fmt.Errorf("github: list tags: %w", err)
		}
		for _, n := range resp.Repository.Refs.Nodes {
			t := tag{Name: n.Name}
			t.Commit.SHA = n.Target.commit()
			all = append(all, t)
		}
		page := resp.Repository.Refs.PageInfo
		if !page.HasNextPage || !provider.Deep(ctx) {
			return all, nil
		}
		end := page.EndCursor
		cursor = &end
	}
}

// listAll fetches the first page of pathFor(page), or every page when ctx
// requests a deep lookup, stopping at the first short page (the last one). The
// releases endpoint is newest-first, so the shallow first page holds the latest
// release; tags are listed through listTags (GraphQL-ordered or REST + fallback)
// rather than here. Deep lookup trades requests for completeness across a
// repository's whole history.
func listAll[T any](
	ctx context.Context,
	rest *restClient,
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
		if len(batch) < perPage || !provider.Deep(ctx) {
			return all, nil // short page = last; shallow = stop after page one
		}
	}
}

// Commit resolves a tag to its peeled commit SHA, satisfying provider.Committer.
// The /commits/{ref} endpoint resolves any ref - including an annotated tag - to
// the commit it points at, so --verify can check a pin even for a tag off the
// discovered page.
func (p *Provider) Commit(ctx context.Context, r provider.Resource, tag string) (string, error) {
	res, ok := r.(resource)
	if !ok {
		return "", fmt.Errorf("github: invalid resource %T", r)
	}
	rest := p.client()

	var commit struct {
		SHA string `json:"sha"`
	}
	path := fmt.Sprintf("repos/%s/%s/commits/%s", res.owner, res.name, tag)
	if err := rest.DoWithContext(ctx, http.MethodGet, path, nil, &commit); err != nil {
		return "", fmt.Errorf("github: resolve commit for %s: %w", tag, err)
	}
	return commit.SHA, nil
}

// tagCommits maps tag name to its peeled commit SHA, for resolving the commit a
// release points at.
func (p *Provider) tagCommits(ctx context.Context, res resource) (map[string]string, error) {
	tags, err := p.listTags(ctx, res)
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
