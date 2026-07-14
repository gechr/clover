package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

type digestKey struct {
	Host       string
	AuthHost   string
	Repository string
	Platform   string
	Tag        string
}

// Digest resolves the content digest a tag points at. By default it returns the
// multi-arch index digest from the Docker-Content-Digest header (a HEAD). When
// repo.Platform is set, it instead returns that os/arch's manifest digest from
// the index - what `docker pull --platform` would pin.
func (c *Client) Digest(ctx context.Context, repo Repo, tag string) (string, error) {
	key := repo.digestKey(tag)
	if digest, ok := c.cachedDigest(key); ok {
		return digest, nil
	}

	if repo.Platform != "" {
		digest, err := c.digestForPlatform(ctx, repo, tag)
		if err != nil {
			return "", err
		}
		c.storeDigest(key, digest)
		return digest, nil
	}

	resp, err := c.fetchManifest(ctx, http.MethodHead, repo, tag)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.StatusErr("head manifest for "+tag, resp)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("%s: registry returned no digest for %s", c.label, tag)
	}
	c.storeDigest(key, digest)
	return digest, nil
}

// fetchManifest issues method against the tag's manifest, performing the
// bearer-token challenge the same way tag discovery does and retrying once with
// the token. The caller closes the response body.
func (c *Client) fetchManifest(
	ctx context.Context,
	method string,
	repo Repo,
	tag string,
) (*http.Response, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", repo.Host, repo.Repository, tag)
	return c.getWithChallenge(ctx, method, url, repo, acceptManifests)
}

// getWithChallenge issues a registry request, performing the bearer-token
// challenge and retrying once with the resulting repository token. The caller
// closes the response body.
func (c *Client) getWithChallenge(
	ctx context.Context,
	method, url string,
	repo Repo,
	accept string,
) (*http.Response, error) {
	token := c.cachedRepoToken(repo)

	resp, err := c.send(ctx, method, url, token, accept)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		if token != "" {
			c.forgetRepoToken(repo)
		}
		token, terr := c.fetchToken(ctx, challenge, repo)
		if terr != nil {
			return nil, terr
		}
		if resp, err = c.send(ctx, method, url, token, accept); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// digestForPlatform GETs the tag's manifest and returns the digest of the
// manifest matching repo.Platform (os/arch) from a multi-arch index. A
// single-arch image is not an index (no manifests array), so its sole digest is
// returned from the header.
func (c *Client) digestForPlatform(ctx context.Context, repo Repo, tag string) (string, error) {
	resp, err := c.fetchManifest(ctx, http.MethodGet, repo, tag)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.StatusErr("get manifest for "+tag, resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%s: read manifest for %s: %w", c.label, tag, err)
	}
	var index struct {
		Manifests []struct {
			Digest   string `json:"digest"`
			Platform struct {
				OS   string `json:"os"`
				Arch string `json:"architecture"`
			} `json:"platform"`
		} `json:"manifests"`
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return "", fmt.Errorf("%s: parse manifest for %s: %w", c.label, tag, err)
	}

	// A single-arch image is not an index (no manifests array). Its sole digest
	// still only satisfies the pin when the image's own platform matches - what
	// `docker pull --platform` enforces - so the config blob's os/architecture
	// is checked before the header digest is returned.
	if len(index.Manifests) == 0 {
		digest := resp.Header.Get("Docker-Content-Digest")
		if digest == "" {
			return "", fmt.Errorf("%s: registry returned no digest for %s", c.label, tag)
		}
		if err := c.checkPlatform(ctx, repo, tag, index.Config.Digest); err != nil {
			return "", err
		}
		return digest, nil
	}

	os, arch, _ := strings.Cut(repo.Platform, "/")
	for _, m := range index.Manifests {
		if m.Platform.OS == os && m.Platform.Arch == arch {
			return m.Digest, nil
		}
	}
	return "", fmt.Errorf("%s: no manifest for platform %s in %s", c.label, repo.Platform, tag)
}

// checkPlatform verifies a single-arch image's config blob against the pinned
// platform. A manifest without a config digest (a legacy schema) cannot be
// checked and is tolerated, keeping the pre-check behavior for images the
// modern media types do not describe.
func (c *Client) checkPlatform(
	ctx context.Context,
	repo Repo,
	tag, configDigest string,
) error {
	if configDigest == "" {
		return nil
	}
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", repo.Host, repo.Repository, configDigest)
	resp, err := c.getWithChallenge(ctx, http.MethodGet, url, repo, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return c.StatusErr("get image config for "+tag, resp)
	}

	var config struct {
		OS   string `json:"os"`
		Arch string `json:"architecture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return fmt.Errorf("%s: parse image config for %s: %w", c.label, tag, err)
	}
	if got := config.OS + "/" + config.Arch; got != repo.Platform {
		return fmt.Errorf("%s: %s is %s, not %s", c.label, tag, got, repo.Platform)
	}
	return nil
}

func (c *Client) cachedDigest(key digestKey) (string, bool) {
	c.digestMu.Lock()
	defer c.digestMu.Unlock()
	digest, ok := c.digests[key]
	return digest, ok
}

func (c *Client) storeDigest(key digestKey, digest string) {
	c.digestMu.Lock()
	defer c.digestMu.Unlock()
	if c.digests == nil {
		c.digests = make(map[digestKey]string)
	}
	c.digests[key] = digest
}

func (r Repo) digestKey(tag string) digestKey {
	return digestKey{
		Host:       r.Host,
		AuthHost:   r.authHost(),
		Repository: r.Repository,
		Platform:   r.Platform,
		Tag:        tag,
	}
}
