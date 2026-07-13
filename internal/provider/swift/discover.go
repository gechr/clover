package swift

import (
	"cmp"
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
	// host is the web origin for Swift, used as the Describe label and the
	// Identify home page.
	host = "swift.org"
	// releasesURL is the public release index: every Swift release, oldest-first,
	// in one JSON response.
	releasesURL = constant.SchemeHTTPS + "www." + host + "/api/v1/install/releases.json"
)

// release is the subset of an index entry the provider reads: the bare version
// (5.10, 6.3.3), the upstream tag (swift-6.3.3-RELEASE), the publication date
// feeding cooldown, and the platform entries whose SDK checksums become free
// asset digests.
type release struct {
	Name      string            `json:"name"`
	Tag       string            `json:"tag"`
	Date      dates.ReleaseTime `json:"date"`
	Platforms []platform        `json:"platforms"`
}

// platform is one platform entry of a release. Only the SDK entries (static-sdk,
// wasm-sdk, android-sdk) carry a checksum; the toolchain entries list installable
// targets with no digest and yield no asset.
type platform struct {
	Platform string `json:"platform"`
	Checksum string `json:"checksum"`
}

// Discover lists candidate Swift versions, reading the release index in one
// fetch. The index holds the whole release history in a single response, so
// there is no pagination and nothing is ever left unread - --deep has no work to
// do here.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	if _, ok := r.(resource); !ok {
		return nil, fmt.Errorf("swift: invalid resource %T", r)
	}

	releases, err := p.fetch(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		if rel.Name == "" {
			continue
		}
		candidates = append(candidates, candidate(rel))
	}
	return candidates, nil
}

// fetch downloads and decodes the release index.
func (p *Provider) fetch(ctx context.Context) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("swift: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("swift: fetch release index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError("swift: list releases", resp)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("swift: decode release index: %w", err)
	}
	return releases, nil
}

// candidate builds a model.Candidate from a release: the bare name is the
// version (a two-component name like 5.10 still parses), the tag is the upstream
// ref, Date feeds cooldown, and each SDK platform entry with a checksum becomes
// an asset carrying the inline sha256 so a follower can source its digest for
// free with no download. The asset Name is the stable platform key (static-sdk).
func candidate(rel release) model.Candidate {
	semver, _ := version.Parse(rel.Name)
	c := model.Candidate{
		Version:     rel.Name,
		Semver:      semver,
		Ref:         cmp.Or(rel.Tag, rel.Name),
		PublishedAt: rel.Date.Time,
	}
	for _, plat := range rel.Platforms {
		if plat.Checksum == "" {
			continue
		}
		c.Assets = append(c.Assets, model.Asset{
			Name:   plat.Platform,
			Digest: constant.DigestSha256 + plat.Checksum,
		})
	}
	return c
}
