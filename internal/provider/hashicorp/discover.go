package hashicorp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	"github.com/gechr/x/set"
)

const (
	// host is the web origin for a product's release pages, also the Describe
	// label, Linker URL, and truncation-hint root.
	host = "releases.hashicorp.com"
	// apiBaseURL is the public releases metadata API, listing a product's
	// releases newest-first.
	apiBaseURL = "https://api.releases.hashicorp.com/v1/releases"

	// licenseClassOSS and licenseClassEnterprise are the two values the releases
	// API reports for a release's license_class. An empty class is treated as OSS.
	licenseClassOSS        = "oss"
	licenseClassEnterprise = "enterprise"
)

const (
	// pageLimit is the page size requested from the list endpoint.
	pageLimit = 20
	// maxPages bounds a deep walk so a product with a long history cannot make a
	// run page through the entire archive; the common case is satisfied far sooner.
	maxPages = 25
)

// release is the subset of a HashiCorp release object the provider reads.
type release struct {
	Version          string `json:"version"`
	IsPrerelease     bool   `json:"is_prerelease"`
	LicenseClass     string `json:"license_class"`
	TimestampCreated string `json:"timestamp_created"`
}

// Discover lists candidate versions for a product, reading the releases API
// newest-first. A shallow lookup reads only the first page - the latest release
// is always on it, since the listing is newest-first; --deep walks the cursor to
// exhaustion for a constraint pinned to an older release.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("hashicorp: invalid resource %T", r)
	}

	releases, truncated, err := p.fetch(ctx, res.product)
	if err != nil {
		return nil, err
	}
	// Report a truncated shallow lookup so a constrained marker that finds no
	// candidate can be hinted toward --deep. The listing is newest-first, so this
	// never drives the blanket "missed newer versions" warning (see RecencyOrdered).
	if truncated {
		provider.NoteTruncated(ctx, p.Describe(res), "https://"+host+"/"+res.product)
	}

	candidates := make([]model.Candidate, 0, len(releases))
	seen := make(set.Set[string], len(releases))
	for _, rel := range releases {
		if rel.Version == "" {
			continue
		}
		base, suffix, _ := strings.Cut(rel.Version, "+")
		if !res.matches(rel, suffix) {
			continue
		}
		// A build flavor renders the full +metadata version; otherwise the bare
		// semver, so a release's enterprise flavors (+ent, +ent.hsm, ...) collapse
		// to one candidate.
		ver := base
		if res.build != "" {
			ver = rel.Version
		}
		if seen.Contains(ver) {
			continue
		}
		seen.Add(ver)
		candidates = append(candidates, candidate(ver, rel))
	}
	return candidates, nil
}

// fetch walks the newest-first list endpoint, returning the releases gathered and
// whether more remained unread. A shallow lookup reads one page; a deep lookup
// follows the timestamp cursor up to maxPages, stopping early on a short page that
// signals the end of the archive.
func (p *Provider) fetch(ctx context.Context, product string) ([]release, bool, error) {
	var all []release
	after := ""
	for range maxPages {
		listURL := fmt.Sprintf("%s/%s?limit=%d", apiBaseURL, product, pageLimit)
		if after != "" {
			listURL += "&after=" + url.QueryEscape(after)
		}

		releases, err := p.page(ctx, listURL, product)
		if err != nil {
			return nil, false, err
		}
		all = append(all, releases...)

		// A short page ends the archive; a full one leaves more unread.
		full := len(releases) == pageLimit
		// Shallow reads only the newest page, truncated when it was full.
		if !provider.Deep(ctx) || !full {
			return all, !provider.Deep(ctx) && full, nil
		}
		// Page older releases using the last item's creation timestamp as cursor.
		last := releases[len(releases)-1]
		if last.TimestampCreated == "" {
			return all, false, nil
		}
		after = last.TimestampCreated
	}
	// A deep lookup that exhausted the page cap has older releases unread, but
	// the truncation signal only drives a "pass --deep" hint - useless on a run
	// that is already deep - so it is not reported here (matching gitlab).
	return all, false, nil
}

// page fetches and decodes one page of the list endpoint.
func (p *Provider) page(ctx context.Context, listURL, product string) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, fmt.Errorf("hashicorp: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hashicorp: fetch %s releases: %w", product, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError(fmt.Sprintf("hashicorp: list %s releases", product), resp)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("hashicorp: decode releases: %w", err)
	}
	return releases, nil
}

// candidate builds a model.Candidate from a release for the rendered version
// (bare semver, or the full +metadata form for a build flavor), parsing it for
// comparison and carrying the prerelease flag and creation date the API supplied
// for free. The Semver is parsed from the base, ignoring any build metadata; a
// version that is not semver-shaped yields a nil Semver and is skipped by
// selection.
func candidate(ver string, rel release) model.Candidate {
	base, _, _ := strings.Cut(ver, "+")
	semver, _ := version.Parse(base)
	published, _ := dates.ParseReleaseTime(rel.TimestampCreated)
	return model.Candidate{
		Version:     ver,
		Semver:      semver,
		Prerelease:  rel.IsPrerelease,
		PublishedAt: published,
		Ref:         ver,
	}
}

// matches reports whether a release belongs to the requested edition. A build
// flavor matches releases whose +metadata suffix equals it exactly (an inherently
// enterprise build); enterprise matches the enterprise license class; the default
// matches open-source builds (oss, or an empty/unspecified class).
func (res resource) matches(rel release, suffix string) bool {
	if res.build != "" {
		return suffix == res.build
	}
	if res.enterprise {
		return rel.LicenseClass == licenseClassEnterprise
	}
	return rel.LicenseClass == licenseClassOSS || rel.LicenseClass == ""
}
