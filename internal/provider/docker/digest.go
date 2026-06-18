package docker

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gechr/clover/internal/provider"
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
func (p *Provider) Digest(ctx context.Context, r provider.Resource, tag string) (string, error) {
	ref, ok := r.(reference)
	if !ok {
		return "", fmt.Errorf("docker: invalid resource %T", r)
	}
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", ref.registryV2Host(), ref.repository, tag)

	resp, err := p.send(ctx, http.MethodHead, url, "", acceptManifests)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		token, terr := p.fetchToken(ctx, challenge, ref)
		if terr != nil {
			return "", terr
		}
		if resp, err = p.send(ctx, http.MethodHead, url, token, acceptManifests); err != nil {
			return "", err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker: head manifest for %s: %s", tag, resp.Status)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("docker: registry returned no digest for %s", tag)
	}
	return digest, nil
}
