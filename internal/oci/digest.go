package oci

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// acceptManifests asks for the multi-arch index media types first, so the
// registry returns the index digest a tag pin uses rather than one platform's
// manifest digest.
var acceptManifests = strings.Join([]string{
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
}, ", ")

// Digest resolves the content digest a tag points at, from the registry's
// Docker-Content-Digest header. It performs the bearer-token challenge the same
// way tag discovery does.
func (c *Client) Digest(ctx context.Context, repo Repo, tag string) (string, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", repo.Host, repo.Repository, tag)

	resp, err := c.send(ctx, http.MethodHead, url, "", acceptManifests)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		token, terr := c.fetchToken(ctx, challenge, repo)
		if terr != nil {
			return "", terr
		}
		if resp, err = c.send(ctx, http.MethodHead, url, token, acceptManifests); err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.StatusErr("head manifest for "+tag, resp)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("%s: registry returned no digest for %s", c.label, tag)
	}
	return digest, nil
}
