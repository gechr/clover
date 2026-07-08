package zig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

const (
	// host is the web origin for Zig, used as the Describe label and the Linker
	// URL root.
	host = "ziglang.org"
	// indexURL is the public download index: every Zig release keyed by version in
	// one JSON object, each with its per-platform checksums and publication date.
	indexURL = "https://ziglang.org/download/index.json"
	// downloadBase is the directory a release's files are served from; the Linker
	// hangs the version off it.
	downloadBase = "https://ziglang.org/download/"
	// masterKey is the moving nightly pointer in the index, not a release, so it is
	// skipped.
	masterKey = "master"
)

// asset is one downloadable artifact of a release, as returned by a platform
// sub-object of an index entry. The version-level metadata fields (docs, notes,
// version, ...) are strings, so they fail this struct-unmarshal and are skipped.
type asset struct {
	Tarball string `json:"tarball"`
	Shasum  string `json:"shasum"`
}

// entry is the subset of an index entry the provider reads. Date feeds cooldown
// and tolerates a missing or date-only value; the platform sub-objects are read
// separately from the raw field map.
type entry struct {
	Date dates.ReleaseTime `json:"date"`
}

// Discover lists candidate Zig versions, reading the download index in one fetch.
// The index holds the whole release history in a single object keyed by version,
// so there is no pagination and nothing is ever left unread - --deep has no work
// to do here. The map iteration order is nondeterministic, so candidates are
// returned unordered; selection sorts regardless.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	if _, ok := r.(resource); !ok {
		return nil, fmt.Errorf("zig: invalid resource %T", r)
	}

	index, err := p.fetch(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(index))
	for key, raw := range index {
		if key == masterKey {
			continue // the nightly pointer, not a release
		}
		semver, err := version.Parse(key)
		if err != nil {
			continue // defensive: the key is not a version
		}
		c, err := candidate(key, semver, raw)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	return candidates, nil
}

// fetch downloads and decodes the download index into its version-keyed entries.
func (p *Provider) fetch(ctx context.Context) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("zig: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zig: fetch download index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError("zig: list releases", resp)
	}

	var index map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("zig: decode download index: %w", err)
	}
	return index, nil
}

// candidate builds a model.Candidate from an index entry: the key is the version
// (clean semver, so Version and Ref are the same), Date feeds cooldown, and each
// platform sub-object with a tarball becomes an asset carrying the inline sha256
// so a follower can source its digest for free with no download. The asset Name is
// the stable platform key (x86_64-linux), not the tarball filename, which drifts
// across versions (zig-x86_64-linux-0.16.0 vs zig-linux-x86_64-0.12.0).
func candidate(key string, semver *version.Version, raw json.RawMessage) (model.Candidate, error) {
	var meta entry
	if err := json.Unmarshal(raw, &meta); err != nil {
		return model.Candidate{}, fmt.Errorf("zig: decode release %q: %w", key, err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return model.Candidate{}, fmt.Errorf("zig: decode release %q: %w", key, err)
	}

	c := model.Candidate{
		Version:     key,
		Semver:      semver,
		Ref:         key,
		PublishedAt: meta.Date.Time,
	}
	for platform, rawField := range fields {
		var a asset
		if err := json.Unmarshal(rawField, &a); err != nil {
			continue // a string metadata field (docs, notes, version, ...)
		}
		if a.Tarball == "" {
			continue
		}
		c.Assets = append(c.Assets, model.Asset{
			Name:   platform,
			Digest: constant.DigestSha256 + a.Shasum,
			URL:    a.Tarball,
		})
	}
	return c, nil
}
