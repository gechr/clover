package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

const (
	// host is the web origin for npm, used as the Describe label and the Linker
	// URL root.
	host = "npmjs.com"
	// registryURL is the public npm registry: one packument per package, holding
	// every published version and its publication date.
	registryURL = "https://registry.npmjs.org"
)

// packument is the subset of a registry packument the provider reads. Versions
// holds one entry per published version; DistTags maps each channel pointer
// (latest, beta, ...) to the version it names; Time dates each version and also
// carries the non-version created/modified keys, which are never looked up. An
// unpublished version lingers in Time but leaves Versions, so Versions drives
// the listing.
type packument struct {
	Versions map[string]versionEntry `json:"versions"`
	DistTags map[string]string       `json:"dist-tags"`
	Time     timeMap                 `json:"time"`
}

// timeMap is the packument's time map, decoded tolerantly: the registry mixes
// per-version date strings with the non-version created/modified keys and, for
// a fully unpublished package, an "unpublished" object. A value that is not a
// date string is dropped rather than failing the whole decode, leaving that
// version undated - cooldown then goes inert for it, which beats a
// wrong-but-present date.
type timeMap map[string]dates.ReleaseTime

// UnmarshalJSON decodes the map value by value, keeping only the entries that
// parse as dates.
func (m *timeMap) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = make(timeMap, len(raw))
	for k, v := range raw {
		var rt dates.ReleaseTime
		if err := json.Unmarshal(v, &rt); err != nil {
			continue
		}
		(*m)[k] = rt
	}
	return nil
}

// versionEntry is one published version's record: the artifact block and the
// deprecation marker.
type versionEntry struct {
	Dist       dist            `json:"dist"`
	Deprecated json.RawMessage `json:"deprecated"`
}

// deprecated reports whether the version is marked deprecated. The field is
// normally the deprecation message string, but an absent field, empty string,
// null, or false all mean active, so anything else deprecates.
func (e versionEntry) deprecated() bool {
	switch string(e.Deprecated) {
	case "", `""`, "null", "false":
		return false
	}
	return true
}

// dist is a version's artifact record; only the tarball URL is read, backing
// the candidate's sole asset. The registry's own digests are sha1 and sha512,
// which clover does not consume.
type dist struct {
	Tarball string `json:"tarball"`
}

// Discover lists candidate versions of a package, reading its packument in one
// fetch. The packument holds the whole version history in a single response -
// keyed in publish order, not version order - so there is no pagination,
// nothing is ever left unread, and --deep has no work to do here. Deprecated
// versions are dropped unless the directive keeps them eligible.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("npm: invalid resource %T", r)
	}

	pkg, err := p.fetch(ctx, res)
	if err != nil {
		return nil, err
	}

	// A dist-tag narrows the listing to the one version the registry's channel
	// pointer names. A tag the registry does not carry is its own error - the
	// package itself exists, a missing one surfaces as the fetch's 404.
	if res.distTag != "" {
		v, ok := pkg.DistTags[res.distTag]
		if !ok {
			return nil, fmt.Errorf("npm: package %q has no dist-tag %q", res.pkg, res.distTag)
		}
		entry := pkg.Versions[v]
		// A deprecated channel pointer yields no candidates rather than an
		// error, mirroring the listing's gate below.
		if !res.deprecated && entry.deprecated() {
			return nil, nil
		}
		return []model.Candidate{candidate(v, entry, pkg.Time[v])}, nil
	}

	candidates := make([]model.Candidate, 0, len(pkg.Versions))
	for v, entry := range pkg.Versions {
		if v == "" {
			continue
		}
		if !res.deprecated && entry.deprecated() {
			continue
		}
		candidates = append(candidates, candidate(v, entry, pkg.Time[v]))
	}
	return candidates, nil
}

// fetch downloads and decodes a package's packument from the resource's
// registry. The name is path-escaped whole, yielding the @scope%2Fname form the
// registry documents for scoped packages.
func (p *Provider) fetch(ctx context.Context, res resource) (packument, error) {
	endpoint := res.registry + "/" + url.PathEscape(res.pkg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return packument{}, fmt.Errorf("npm: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return packument{}, fmt.Errorf("npm: fetch packument: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return packument{}, provider.StatusError("npm: get package "+res.pkg, resp)
	}

	var pkg packument
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return packument{}, fmt.Errorf("npm: decode packument: %w", err)
	}
	return pkg, nil
}

// candidate builds a model.Candidate from a version, its packument entry, and
// its publication time. npm versions are bare semver, so Version and Ref carry
// the same string; the tarball is the version's sole asset, letting a sha256
// follower download and hash it. A version missing from the time map leaves
// PublishedAt zero, so cooldown goes inert rather than trusting a wrong date.
func candidate(v string, entry versionEntry, published dates.ReleaseTime) model.Candidate {
	c := model.NewCandidate(v)
	c.PublishedAt = published.Time
	if entry.Dist.Tarball != "" {
		c.Assets = []model.Asset{{
			Name: path.Base(entry.Dist.Tarball),
			URL:  entry.Dist.Tarball,
		}}
	}
	return c
}
