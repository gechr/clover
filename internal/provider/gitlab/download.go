package gitlab

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
// provider.AssetDownloader. With a credential the asset is read through its
// api/v4 link, which authorizes a private project's download where the browser
// URL 401s; anonymously the browser URL is fetched directly. The bearer is
// attached only when the API URL shares the host's API origin - a release link
// may point at an arbitrary external host, which must not receive the token -
// and any redirect off that origin (a subdomain object store, a scheme
// downgrade) drops the header.
func (p *Provider) DownloadAsset(
	ctx context.Context,
	r provider.Resource,
	asset model.Asset,
) (io.ReadCloser, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("gitlab: invalid resource %T", r)
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
		return nil, fmt.Errorf("gitlab: build request: %w", err)
	}
	// Asset bytes are hashed once and would blow the cache's per-entry cap, so
	// keep them out of the HTTP cache.
	req.Header.Set("Cache-Control", "no-store")
	if token != "" {
		req.Header.Set("Authorization", forge.Bearer(token))
	}

	client := *p.rest.HTTPClient()
	client.CheckRedirect = forge.DropAuthRedirect
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: download %s: %w", asset.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("gitlab: download %s: %s", asset.Name, resp.Status)
	}
	return resp.Body, nil
}
