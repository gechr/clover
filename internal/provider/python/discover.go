package python

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

const (
	// host is the web origin for Python, used as the Describe label and the
	// Linker URL root.
	host = "python.org"
	// apiURL is the public downloads API: every published CPython release in one
	// JSON array, each with its publication date and prerelease flag.
	apiURL = "https://www.python.org/api/v2/downloads/release/?is_published=true"
	// releasePath is the release-page directory a slug hangs off, for the Linker.
	releasePath = "https://www.python.org/downloads/release/"
	// versionPrefix is the "Python " prefix every release name carries (e.g.
	// "Python 3.14.6"). It is stripped before parsing.
	versionPrefix = "Python "
)

// release is the subset of a downloads-API entry the provider reads. The version
// lives in Name ("Python 3.14.6"); PreRelease flags an alpha/beta/rc build; Slug
// backs the release-page link; ReleaseDate feeds cooldown and tolerates a null.
type release struct {
	Name        string            `json:"name"`
	Slug        string            `json:"slug"`
	PreRelease  bool              `json:"pre_release"`
	ReleaseDate dates.ReleaseTime `json:"release_date"`
}

// Discover lists candidate CPython versions, reading the downloads API in one
// fetch. The API returns the whole release history in a single response, so
// there is no pagination and nothing is ever left unread - --deep has no work to
// do here.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	if _, ok := r.(resource); !ok {
		return nil, fmt.Errorf("python: invalid resource %T", r)
	}

	releases, err := p.fetch(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		bare := strings.TrimPrefix(rel.Name, versionPrefix)
		if bare == rel.Name || bare == "" {
			continue // not a "Python <version>" release name
		}
		semver, err := version.Parse(bare)
		if err != nil {
			// The API mixes in non-interpreter rows (e.g. "Python install
			// manager 26.3"), whose remainder is not a version; drop them rather
			// than surface an unparseable candidate.
			continue
		}
		candidates = append(candidates, candidate(rel, bare, semver))
	}
	return candidates, nil
}

// fetch downloads and decodes the downloads API.
func (p *Provider) fetch(ctx context.Context) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("python: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("python: fetch downloads API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError("python: list releases", resp)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("python: decode downloads API: %w", err)
	}
	return releases, nil
}

// candidate builds a model.Candidate from a release, its de-prefixed version,
// and that version's parse. Version is the parsed semver's canonical form, which
// restores the dash a python.org prerelease omits (3.15.0b3 -> 3.15.0-b3) so it
// orders and scheme-matches like any other prerelease; the raw de-prefixed form
// stays on Ref and the API's prerelease flag on Prerelease. PublishedAt carries
// the release date for cooldown, and the slug backs the release-page link.
func candidate(rel release, bare string, semver *version.Version) model.Candidate {
	return model.Candidate{
		Version:     semver.String(),
		Semver:      semver,
		Ref:         bare,
		Prerelease:  rel.PreRelease,
		PublishedAt: rel.ReleaseDate.Time,
		Meta:        map[string]string{metaSlug: rel.Slug},
	}
}
