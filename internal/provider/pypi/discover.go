package pypi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
	xstrings "github.com/gechr/x/strings"
)

const (
	// host is the web origin for PyPI, used as the Describe label root and the
	// Linker URL host.
	host = "pypi.org"
	// apiPath is the JSON API root: apiPath/<package>/json returns the package's
	// whole release history in one response.
	apiPath = "https://pypi.org/pypi/"
	// projectPath is the project-page root the Linker builds from.
	projectPath = "https://pypi.org/project/"
)

// listing is the subset of a JSON API response the provider reads: the map of
// version to its uploaded files.
type listing struct {
	Releases map[string][]file `json:"releases"`
}

// file is the subset of an uploaded file's record the provider reads: the
// upload time feeds cooldown, the yanked flag drops withdrawn versions, and
// the filename plus sha256 digest surface as an asset.
type file struct {
	Filename   string            `json:"filename"`
	URL        string            `json:"url"`
	Yanked     bool              `json:"yanked"`
	UploadTime dates.ReleaseTime `json:"upload_time_iso_8601"`
	Digests    digests           `json:"digests"`
}

// digests is the digest map of an uploaded file; sha256 is the only entry
// clover consumes.
type digests struct {
	SHA256 string `json:"sha256"`
}

// Discover lists a package's candidate versions, reading the JSON API in one
// fetch. The API returns the whole release history in a single response, so
// there is no pagination and nothing is ever left unread - --deep has no work
// to do here. The map arrives unordered, so candidates are sorted naturally
// for a deterministic listing.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("pypi: invalid resource %T", r)
	}

	releases, err := p.fetch(ctx, res.name)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for raw, files := range releases {
		live := alive(files)
		if len(live) == 0 {
			continue // no files, or every file yanked: not installable
		}
		semver, err := version.Parse(raw)
		if err != nil {
			// A .dev or .post suffix or an epoch is not semver-shaped; drop it
			// rather than surface an unparseable candidate.
			continue
		}
		candidates = append(candidates, candidate(raw, semver, live))
	}
	slices.SortFunc(candidates, func(a, b model.Candidate) int {
		// The ref breaks a tie between two raw spellings of one canonical
		// version (1.0 and 1.0.0), keeping the listing deterministic.
		return cmp.Or(
			xstrings.CompareNatural(a.Version, b.Version),
			xstrings.CompareNatural(a.Ref, b.Ref),
		)
	})
	return candidates, nil
}

// fetch downloads and decodes a package's JSON API listing.
func (p *Provider) fetch(ctx context.Context, name string) (map[string][]file, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiPath+name+"/json", nil)
	if err != nil {
		return nil, fmt.Errorf("pypi: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pypi: fetch package listing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError("pypi: list "+name+" releases", resp)
	}

	var l listing
	if err := json.NewDecoder(resp.Body).Decode(&l); err != nil {
		return nil, fmt.Errorf("pypi: decode package listing: %w", err)
	}
	return l.Releases, nil
}

// alive returns the files still offered for a version, dropping yanked ones.
func alive(files []file) []file {
	return slices.DeleteFunc(slices.Clone(files), func(f file) bool { return f.Yanked })
}

// candidate builds a model.Candidate from a version, its parse, and its live
// files. Version is the parsed semver's canonical form, which restores the
// dash a PEP 440 prerelease omits (0.5.30rc1 -> 0.5.30-rc1) so it orders and
// scheme-matches like any other prerelease; the raw PyPI form stays on Ref.
// PublishedAt is the earliest upload time for cooldown, and each file surfaces
// as an asset with its sha256 digest, letting a follower source a checksum
// without a download.
func candidate(raw string, semver *version.Version, files []file) model.Candidate {
	var published time.Time
	assets := make([]model.Asset, 0, len(files))
	for _, f := range files {
		if uploaded := f.UploadTime.Time; !uploaded.IsZero() &&
			(published.IsZero() || uploaded.Before(published)) {
			published = uploaded
		}
		digest := f.Digests.SHA256
		if digest != "" {
			digest = "sha256:" + digest
		}
		assets = append(assets, model.Asset{Name: f.Filename, Digest: digest, URL: f.URL})
	}
	return model.Candidate{
		Version:     semver.String(),
		Semver:      semver,
		Ref:         raw,
		PublishedAt: published,
		Assets:      assets,
	}
}
