package golang

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/version"
)

const (
	// host is the web origin for Go, used as the Describe label and the Linker
	// URL root.
	host = "go.dev"
	// indexURL is the public download index: every Go release, newest-first, in
	// one JSON response. include=all keeps release candidates and betas, which the
	// prerelease gate filters unless they are opted in.
	indexURL = "https://go.dev/dl/?mode=json&include=all"
	// downloadBase is the directory every release file is served from; the index
	// gives filenames, not full URLs, so the download URL is derived from it.
	downloadBase = "https://go.dev/dl/"
	// versionPrefix is the "go" prefix every go.dev version carries (e.g.
	// "go1.26.5"). It is stripped so the resolved value is clean semver.
	versionPrefix = "go"
)

// file is one downloadable artifact of a release, as returned by the download
// index. os/arch are empty for the source archive.
type file struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	SHA256   string `json:"sha256"`
	Kind     string `json:"kind"`
}

// release is the subset of a download-index entry the provider reads.
type release struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []file `json:"files"`
}

// Discover lists candidate Go versions, reading the download index in one fetch.
// The index holds the whole release history newest-first in a single response,
// so there is no pagination and nothing is ever left unread - --deep has no work
// to do here.
func (p *Provider) Discover(ctx context.Context, r provider.Resource) ([]model.Candidate, error) {
	if _, ok := r.(resource); !ok {
		return nil, fmt.Errorf("go: invalid resource %T", r)
	}

	releases, err := p.fetch(ctx)
	if err != nil {
		return nil, err
	}

	candidates := make([]model.Candidate, 0, len(releases))
	for _, rel := range releases {
		if rel.Version == "" {
			continue
		}
		candidates = append(candidates, candidate(rel))
	}
	return candidates, nil
}

// fetch downloads and decodes the download index.
func (p *Provider) fetch(ctx context.Context) ([]release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("go: build request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("go: fetch download index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, provider.StatusError("go: list releases", resp)
	}

	var releases []release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("go: decode download index: %w", err)
	}
	return releases, nil
}

// candidate builds a model.Candidate from a release. The ubiquitous "go" prefix
// is stripped so Version is clean semver (matching a bare on-line reference and
// letting <version> render cleanly), while the prefixed form is retained on Ref
// for links. Version is the parsed semver's canonical form, which restores the
// dash a go.dev prerelease omits (go1.27rc1 -> 1.27.0-rc1) so it orders and
// scheme-matches like any other prerelease; the smart rewriter re-precisions it
// back onto the target line's style. Each file with a checksum becomes an asset
// so a follower can source its sha256 for free, no download.
func candidate(rel release) model.Candidate {
	bare := strings.TrimPrefix(rel.Version, versionPrefix)
	semver, _ := version.Parse(bare)

	value := bare
	if semver != nil {
		value = semver.String()
	}

	c := model.Candidate{Version: value, Semver: semver, Ref: rel.Version}
	for _, f := range rel.Files {
		if f.SHA256 == "" {
			continue
		}
		c.Assets = append(c.Assets, model.Asset{
			Name:   f.Filename,
			Digest: constant.DigestSha256 + f.SHA256,
			URL:    downloadBase + f.Filename,
		})
	}
	return c
}
