package helm

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"path"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/dates"
	"github.com/gechr/clover/internal/model"
	xslices "github.com/gechr/x/slices"
	"gopkg.in/yaml.v3"
)

// indexFile is the subset of a Helm repository index.yaml clover reads.
type indexFile struct {
	Entries map[string][]indexEntry `yaml:"entries"`
}

// indexEntry is one published version of a chart in the index.
type indexEntry struct {
	Version string            `yaml:"version"`
	Created dates.ReleaseTime `yaml:"created"`
	Digest  string            `yaml:"digest"`
	URLs    []string          `yaml:"urls"`
}

// discoverIndex fetches a classic repository's index.yaml and lists the named
// chart's versions, carrying each version's release date (for cooldown) and the
// chart-tarball digest and URL (for sourcing a follower's checksum).
func (p *Provider) discoverIndex(ctx context.Context, ref reference) ([]model.Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.indexURL, nil)
	if err != nil {
		return nil, fmt.Errorf("helm: build request: %w", err)
	}
	resp, err := p.client.HTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("helm: fetch %s: %w", ref.indexURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, p.client.StatusErr("fetch index for "+ref.chart, resp)
	}

	var index indexFile
	if err := yaml.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("helm: decode index: %w", err)
	}

	entries, ok := index.Entries[ref.chart]
	if !ok {
		return nil, fmt.Errorf("helm: chart %q not found in %s", ref.chart, ref.baseURL)
	}

	return xslices.Map(entries, func(e indexEntry) model.Candidate {
		return indexCandidate(ref, e)
	}), nil
}

// indexCandidate builds a candidate from an index entry, attaching the chart
// tarball as an asset when the index supplies a digest and URL so a follower can
// source its checksum without a download.
func indexCandidate(ref reference, e indexEntry) model.Candidate {
	c := candidate(e.Version, e.Created.Time)
	if e.Digest != "" && len(e.URLs) > 0 {
		assetURL := resolveURL(ref.baseURL, e.URLs[0])
		c.Assets = []model.Asset{{
			Name:   path.Base(assetURL),
			Digest: normalizeDigest(e.Digest),
			URL:    assetURL,
		}}
	}
	return c
}

// resolveURL resolves a possibly-relative chart URL from the index against the
// repository base. A Helm index may carry absolute or relative URLs; the base is
// treated as a directory (trailing slash) for RFC 3986 resolution, so a
// host-absolute (/charts/x.tgz) or dot-relative (../x.tgz) URL resolves correctly
// rather than being blindly appended to the base path.
func resolveURL(base, raw string) string {
	if strings.Contains(raw, "://") {
		return raw
	}
	b, berr := neturl.Parse(base)
	r, rerr := neturl.Parse(raw)
	if berr != nil || rerr != nil {
		return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(raw, "/")
	}
	b.Path = strings.TrimSuffix(b.Path, "/") + "/"
	return b.ResolveReference(r).String()
}

// normalizeDigest prefixes a bare hex digest with its algorithm, matching the
// sha256:... form clover's checksum sourcing expects. Helm indexes record the
// chart-tarball digest as bare hex.
func normalizeDigest(digest string) string {
	if strings.Contains(digest, ":") {
		return digest
	}
	return constant.DigestSha256 + digest
}
