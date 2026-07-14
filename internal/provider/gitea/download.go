package gitea

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/forge"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// DownloadAsset streams a release asset's content, satisfying
// provider.AssetDownloader. Gitea serves an uploaded asset's bytes at its
// browser URL (/attachments/<uuid>), which honors the API credential - its
// asset API endpoint returns JSON metadata, not bytes - so the browser URL is
// fetched with the credential attached whenever it sits on the forge host
// itself. An external-type asset points off-host and must not receive the
// token, and any redirect off the forge origin (a subdomain object store, a
// scheme downgrade) drops the header.
func (p *Provider) DownloadAsset(
	ctx context.Context,
	r provider.Resource,
	asset model.Asset,
) (io.ReadCloser, error) {
	res, ok := r.(resource)
	if !ok {
		return nil, fmt.Errorf("gitea: invalid resource %T", r)
	}

	var authorization string
	if forge.SameOrigin(constant.SchemeHTTPS+res.host+"/", asset.URL) {
		authorization = p.authorization(ctx, res.host)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("gitea: build request: %w", err)
	}
	// Asset bytes are hashed once and would blow the cache's per-entry cap, so
	// keep them out of the HTTP cache.
	req.Header.Set("Cache-Control", "no-store")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	client := *p.rest.HTTPClient()
	client.CheckRedirect = forge.DropAuthRedirect
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea: download %s: %w", asset.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("gitea: download %s: %s", asset.Name, resp.Status)
	}
	return resp.Body, nil
}
