package github

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	xhttp "github.com/gechr/x/http"
	xslices "github.com/gechr/x/slices"
)

// perPage is the page size, GitHub's ceiling for both the REST tags endpoint and
// a GraphQL refs connection. A shallow lookup reads only the first page - enough
// for selection and respectful of the rate limit - so a tag marker costs one
// request. A deep lookup pages to exhaustion instead.
const perPage = 100

const gqlCacheFreshness = time.Minute

// tagsQuery lists a repository's tags ordered newest-first by their target
// commit date - the order the GitHub web UI shows, which the REST tags endpoint
// cannot produce (it has no sort parameter). An annotated tag's target is a Tag
// object whose own target is the peeled commit; a lightweight tag's target is
// the commit directly. first=100 is GitHub's page ceiling (perPage). query
// filters refs by substring server-side (null means unfiltered), composing with
// the ordering.
const tagsQuery = `query($owner: String!, $name: String!, $cursor: String, $query: String) {
  repository(owner: $owner, name: $name) {
    refs(refPrefix: "refs/tags/", query: $query, first: 100, after: $cursor, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}) {
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
	TagName     string            `json:"tag_name"`
	PublishedAt dates.ReleaseTime `json:"published_at"`
	Draft       bool              `json:"draft"`
	Prerelease  bool              `json:"prerelease"`
	Assets      []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
		URL    string `json:"browser_download_url"`
		APIURL string `json:"url"`
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
	case forge.SourceReleases:
		return p.discoverReleases(ctx, res)
	case forge.SourceTags:
		return p.discoverTags(ctx, res)
	}
	return nil, fmt.Errorf("github: unknown source %q", res.source)
}

// discoverTags lists a repository's tags as candidates. The authenticated
// GraphQL path is ordered newest-first, so its shallow first page already holds
// the latest version. The anonymous REST path is not ordered (the endpoint has
// no sort, and a repo's legacy tag namespace - e.g. golang/go's weekly.* tags -
// can bury every real version past page one), so a full shallow page that yields
// no parsable version escalates to a deep lookup once; a shallow page that did
// parse but left pages unread reports its truncation, since the unordered
// listing may hold the newest version - or a constrained marker's older match -
// on an unread page.
func (p *Provider) discoverTags(ctx context.Context, res resource) ([]model.Candidate, error) {
	tags, truncated, err := p.listTags(ctx, res)
	if err != nil {
		return nil, err
	}
	candidates := candidatesFromTags(tags)

	if truncated && !anyParsable(candidates) {
		tags, truncated, err = p.listTags(provider.WithDeep(ctx, true), res)
		if err != nil {
			return nil, err
		}
		candidates = candidatesFromTags(tags)
	}
	if truncated {
		provider.NoteTruncated(
			ctx,
			p.Describe(res),
			constant.SchemeHTTPS+res.host+"/"+res.owner+"/"+res.name+"/tags",
		)
	}
	return candidates, nil
}

// candidatesFromTags projects discovered tags into candidates.
func candidatesFromTags(tags []tag) []model.Candidate {
	return xslices.Map(tags, func(t tag) model.Candidate {
		return candidate(t.Name, t.Commit.SHA, false, time.Time{}, nil)
	})
}

// anyParsable reports whether any candidate carries a parsable version - the
// signal that a shallow page held at least one real version, so a deep fallback
// would add nothing.
func anyParsable(candidates []model.Candidate) bool {
	return slices.ContainsFunc(candidates, func(c model.Candidate) bool {
		return c.Semver != nil
	})
}

func (p *Provider) discoverReleases(ctx context.Context, res resource) ([]model.Candidate, error) {
	start := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/releases?per_page=%d", res.owner, res.name, perPage),
	)
	releases, _, err := listAll[release](ctx, p.client(), "releases", start, p.credential(res.host))
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
			assets = append(
				assets,
				model.Asset{Name: a.Name, Digest: a.Digest, URL: a.URL, APIURL: a.APIURL},
			)
		}
		candidates = append(
			candidates,
			candidate(
				rel.TagName,
				commits[rel.TagName],
				rel.Prerelease,
				rel.PublishedAt.Time,
				assets,
			),
		)
	}
	return candidates, nil
}

// listTags returns a repository's tags, newest-first. With a credential it uses
// the GraphQL refs API ordered by tag commit date, degrading to the REST
// endpoint when the credential is rejected (a revoked token should not fail a
// lookup anonymous access would serve); anonymously it uses REST directly,
// since GraphQL rejects unauthenticated requests. Shallow reads the first page;
// a deep lookup pages to exhaustion. The second return reports a truncated
// shallow REST lookup - the ordered GraphQL first page already holds the
// newest, so it never counts as truncated.
func (p *Provider) listTags(ctx context.Context, res resource) ([]tag, bool, error) {
	if p.authenticated(res.host) {
		tags, err := p.listTagsGraphQL(ctx, res)
		if err == nil || !unauthorized(err) {
			return tags, false, err
		}
	}
	return p.listTagsREST(ctx, res)
}

// unauthorized reports whether err is an HTTP 401, the signal that a stored
// credential has been revoked or expired upstream.
func unauthorized(err error) bool {
	var httpErr *api.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusUnauthorized
}

// listTagsREST reads the REST tags endpoint. The endpoint has no sort, so its
// order is git's raw ref order - not necessarily newest-first; discoverTags
// guards that with its deep fallback and truncation note.
func (p *Provider) listTagsREST(ctx context.Context, res resource) ([]tag, bool, error) {
	start := apiURL(
		res.host,
		fmt.Sprintf("repos/%s/%s/tags?per_page=%d", res.owner, res.name, perPage),
	)
	return listAll[tag](ctx, p.client(), "tags", start, p.credential(res.host))
}

// listTagsGraphQL reads tags through the GraphQL refs connection, ordered
// newest-first by target commit date. Shallow returns the first page; a deep
// lookup follows the cursor to exhaustion.
func (p *Provider) listTagsGraphQL(ctx context.Context, res resource) ([]tag, error) {
	gql, err := p.gqlClient(res.host)
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
		// Every tag selection can accept contains the hinted tag-prefix or
		// qualifier, so the substring filter only skips tags that could never be
		// selected. The prefix wins when both are set, being the stronger
		// constraint.
		if q := cmp.Or(provider.TagPrefix(ctx), provider.Qualifier(ctx)); q != "" {
			variables["query"] = q
		}
		if err := gql.DoWithContext(
			httpcache.WithCacheableRequest(ctx, gqlCacheFreshness),
			tagsQuery,
			variables,
			&resp,
		); err != nil {
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

// listAll fetches the first page at start, or every page when ctx requests a
// deep lookup, following GitHub's Link header rather than guessing from the item
// count. The releases endpoint is newest-first, so the shallow first page holds
// the latest release; tags are listed through listTags (GraphQL-ordered or REST
// + fallback) rather than here. Deep lookup trades requests for completeness
// across a repository's whole history. The second return reports a truncated
// shallow lookup - a first page with more behind it.
func listAll[T any](
	ctx context.Context,
	rest forge.RESTClient,
	what, start, token string,
) ([]T, bool, error) {
	var all []T
	for url := start; ; {
		var batch []T
		header, err := rest.DoWithContext(ctx, url, forge.Bearer(token), &batch)
		if err != nil {
			return nil, false, fmt.Errorf("github: list %s: %w", what, err)
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

// Commit resolves a tag to its peeled commit SHA, satisfying provider.Committer,
// so --verify can check a pin even for a tag off the discovered page. The lookup
// is namespace-explicit - git/ref/tags/, peeling an annotated tag via git/tags/ -
// because the commits/{ref} endpoint resolves any ref-like string and prefers a
// branch over a same-named tag, which would let a crafted tag borrow a benign
// branch's commit and pass verification.
func (p *Provider) Commit(ctx context.Context, r provider.Resource, tag string) (string, error) {
	res, ok := r.(resource)
	if !ok {
		return "", fmt.Errorf("github: invalid resource %T", r)
	}
	typ, sha, err := p.tagRefObject(ctx, res, tag)
	if err != nil {
		return "", err
	}
	if typ != tagObjectType { // a lightweight tag points straight at the commit
		return sha, nil
	}

	var tagObj struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	url := apiURL(res.host, fmt.Sprintf("repos/%s/%s/git/tags/%s", res.owner, res.name, sha))
	if _, err := p.client().
		DoWithContext(ctx, url, forge.Bearer(p.credential(res.host)), &tagObj); err != nil {
		return "", fmt.Errorf("github: peel tag %s: %w", tag, err)
	}
	return tagObj.Object.SHA, nil
}

// tagObjectType is the git object type of an annotated tag: its ref points at a
// tag object wrapping the commit, where a lightweight tag's ref points at the
// commit directly.
const tagObjectType = "tag"

// tagRefObject resolves refs/tags/<tag> to its target object's type and SHA.
// The lookup is namespace-explicit so a branch sharing the tag's name can never
// be resolved in its place.
func (p *Provider) tagRefObject(
	ctx context.Context,
	res resource,
	tag string,
) (string, string, error) {
	var ref struct {
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	url := apiURL(res.host, fmt.Sprintf("repos/%s/%s/git/ref/tags/%s", res.owner, res.name, tag))
	if _, err := p.client().
		DoWithContext(ctx, url, forge.Bearer(p.credential(res.host)), &ref); err != nil {
		return "", "", fmt.Errorf("github: resolve tag %s: %w", tag, err)
	}
	return ref.Object.Type, ref.Object.SHA, nil
}

// tagCommits maps tag name to its peeled commit SHA, for resolving the commit a
// release points at. A truncated tag listing is not noted here: it only narrows
// the join, blocking action-pinning for a release older than the newest page of
// tags, not the release listing itself.
func (p *Provider) tagCommits(ctx context.Context, res resource) (map[string]string, error) {
	tags, _, err := p.listTags(ctx, res)
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
	c := model.NewCandidate(raw)
	c.Prerelease = prerelease
	c.Commit = commit
	c.PublishedAt = published
	c.Assets = assets
	return c
}
