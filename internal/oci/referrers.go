package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	acceptOCIIndex = "application/vnd.oci.image.index.v1+json"
	maxBlobSize    = 4 << 20
)

type descriptor struct {
	ArtifactType string `json:"artifactType"`
	Digest       string `json:"digest"`
	MediaType    string `json:"mediaType"`
}

type indexManifest struct {
	Manifests []descriptor `json:"manifests"`
}

type artifactManifest struct {
	Layers []descriptor `json:"layers"`
}

// ReferrerArtifacts returns layer contents from referrer artifacts whose
// artifact and layer media types start with artifactTypePrefix.
func (c *Client) ReferrerArtifacts(
	ctx context.Context,
	repo Repo,
	digest, artifactTypePrefix string,
) ([][]byte, error) {
	refs, err := c.referrers(ctx, repo, digest)
	if err != nil {
		return nil, err
	}

	var artifacts [][]byte
	for _, ref := range refs {
		if !strings.HasPrefix(ref.ArtifactType, artifactTypePrefix) {
			continue
		}
		manifest, err := c.referrerManifest(ctx, repo, ref.Digest)
		if err != nil {
			return nil, err
		}
		for _, layer := range manifest.Layers {
			if !strings.HasPrefix(layer.MediaType, artifactTypePrefix) {
				continue
			}
			contents, err := c.blob(ctx, repo, layer.Digest)
			if err != nil {
				return nil, err
			}
			artifacts = append(artifacts, contents)
		}
	}
	return artifacts, nil
}

func (c *Client) referrers(ctx context.Context, repo Repo, digest string) ([]descriptor, error) {
	url := fmt.Sprintf("https://%s/v2/%s/referrers/%s", repo.Host, repo.Repository, digest)
	resp, err := c.getWithChallenge(ctx, http.MethodGet, url, repo, acceptOCIIndex)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
		defer resp.Body.Close()
		index, decodeErr := decodeResponse[indexManifest](c, resp, "get referrers for "+digest)
		return index.Manifests, decodeErr
	}
	_ = resp.Body.Close()

	algorithm, hex, ok := strings.Cut(digest, ":")
	if !ok || algorithm != "sha256" || hex == "" {
		return nil, fmt.Errorf("%s: invalid subject digest %q", c.label, digest)
	}
	resp, err = c.fetchManifest(ctx, http.MethodGet, repo, "sha256-"+hex)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil, nil
	}
	defer resp.Body.Close()
	index, err := decodeResponse[indexManifest](c, resp, "get referrers fallback for "+digest)
	return index.Manifests, err
}

func (c *Client) referrerManifest(
	ctx context.Context,
	repo Repo,
	digest string,
) (artifactManifest, error) {
	resp, err := c.fetchManifest(ctx, http.MethodGet, repo, digest)
	if err != nil {
		return artifactManifest{}, err
	}
	defer resp.Body.Close()
	return decodeResponse[artifactManifest](c, resp, "get referrer manifest for "+digest)
}

func (c *Client) blob(ctx context.Context, repo Repo, digest string) ([]byte, error) {
	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", repo.Host, repo.Repository, digest)
	resp, err := c.getWithChallenge(ctx, http.MethodGet, url, repo, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.StatusErr("get blob for "+digest, resp)
	}
	contents, err := io.ReadAll(io.LimitReader(resp.Body, maxBlobSize+1))
	if err != nil {
		return nil, fmt.Errorf("%s: read blob for %s: %w", c.label, digest, err)
	}
	if len(contents) > maxBlobSize {
		return nil, fmt.Errorf("%s: blob %s exceeds %d bytes", c.label, digest, maxBlobSize)
	}
	return contents, nil
}

func decodeResponse[T any](c *Client, resp *http.Response, action string) (T, error) {
	var value T
	if resp.StatusCode != http.StatusOK {
		return value, c.StatusErr(action, resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&value); err != nil {
		return value, fmt.Errorf("%s: parse %s: %w", c.label, action, err)
	}
	return value, nil
}
