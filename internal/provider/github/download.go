package github

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// DownloadAsset streams a release asset's content, satisfying
// provider.AssetDownloader. With a credential the asset is read through its API
// endpoint (Accept: application/octet-stream), which authorizes a private
// repository's download where the browser URL 404s; anonymously the browser URL
// is fetched directly. The bearer is attached only when the API URL shares the
// host's API origin, so a forged asset URL cannot redirect the token, and Go's
// client already drops the header on the cross-host redirect to the CDN.
func (p *Provider) DownloadAsset(
	ctx context.Context,
	r provider.Resource,
	asset model.Asset,
) (io.ReadCloser, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("github: invalid resource %T", r)
	}

	url := asset.URL
	var token string
	if cred := p.credential(res.host); cred != "" && asset.APIURL != "" &&
		forge.SameOrigin(apiURL(res.host, ""), asset.APIURL) {
		url = asset.APIURL
		token = cred
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	// Asset bytes are hashed once and would blow the cache's per-entry cap, so
	// keep them out of the HTTP cache.
	req.Header.Set("Cache-Control", "no-store")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	p.initClients()
	resp, err := (&http.Client{Transport: p.resolved}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: download %s: %w", asset.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("github: download %s: %s", asset.Name, resp.Status)
	}
	return resp.Body, nil
}
