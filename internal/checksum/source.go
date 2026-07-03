package checksum

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pattern"
	xstrings "github.com/gechr/x/strings"
)

// maxDownload caps a download-and-hash, so the last-resort source never streams
// an unbounded binary.
const maxDownload = 256 << 20 // 256 MiB

// Request describes how to source a follower's sha256: the source method, the
// producer's release assets, an asset-selecting pattern, and an optional
// explicit checksums-file URL.
type Request struct {
	Source  string
	Assets  []model.Asset
	Pattern string
	URL     string
	Version string
}

// Resolve sources a sha256 for the asset the request selects, per its source
// (defaulting to auto). The sources are constant.Sha256{Digest,Checksums,
// Download}; auto tries them in that order, and verify cross-checks the digest
// against a checksums file.
func Resolve(ctx context.Context, client *http.Client, req Request) (string, error) {
	switch cmp.Or(req.Source, constant.Sha256Auto) {
	case constant.Sha256Auto:
		return auto(ctx, client, req)
	case constant.Sha256Digest:
		return digest(req)
	case constant.Sha256Checksums:
		return checksums(ctx, client, req)
	case constant.Sha256Download:
		return downloadAndHash(ctx, client, req)
	case constant.Sha256Verify:
		return verify(ctx, client, req)
	}
	return "", fmt.Errorf("checksum: unknown source %q", req.Source)
}

// auto tries the free digest, then a checksums file, then a download-and-hash.
// An explicit sha256-url is authoritative: once given, a fetch/parse/match
// failure is terminal rather than silently degrading to download-and-hash, so a
// publisher's checksum outage is not masked. A discovered sibling, having no
// user intent behind it, still falls through.
func auto(ctx context.Context, client *http.Client, req Request) (string, error) {
	if sum, err := digest(req); err == nil {
		return sum, nil
	}
	if req.URL != "" {
		return checksums(ctx, client, req)
	}
	if sum, err := checksums(ctx, client, req); err == nil {
		return sum, nil
	}
	return downloadAndHash(ctx, client, req)
}

// digest returns the matched asset's provider-supplied sha256 digest.
func digest(req Request) (string, error) {
	asset, err := matchAsset(req)
	if err != nil {
		return "", err
	}
	sum, ok := strings.CutPrefix(asset.Digest, constant.DigestSha256)
	if !ok || !xstrings.IsSHA256(sum) {
		return "", fmt.Errorf("checksum: asset %q has no sha256 digest", asset.Name)
	}
	return sum, nil
}

// checksums sources the hash from a published checksum file: the explicit
// sha256-url, or a sibling discovered among the release assets.
func checksums(ctx context.Context, client *http.Client, req Request) (string, error) {
	if req.URL != "" {
		return Fetch(ctx, client, req.URL, req.Version, req.Pattern)
	}

	asset, err := matchAsset(req)
	if err != nil {
		return "", err
	}
	url := siblingChecksumURL(req.Assets, asset.Name)
	if url == "" {
		return "", fmt.Errorf(
			"checksum: no %q and no checksums file among the assets",
			constant.DirectiveSha256URL,
		)
	}
	data, err := fetchBody(ctx, client, url, maxSize)
	if err != nil {
		return "", err
	}
	return hashForFile(parse(string(data)), asset.Name)
}

// downloadAndHash downloads the matched asset and computes its sha256.
func downloadAndHash(ctx context.Context, client *http.Client, req Request) (string, error) {
	asset, err := matchAsset(req)
	if err != nil {
		return "", err
	}

	get, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", fmt.Errorf("checksum: build request: %w", err)
	}
	get.Header.Set("Cache-Control", "no-store")
	resp, err := client.Do(get)
	if err != nil {
		return "", fmt.Errorf("checksum: download %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum: download %s: %s", asset.Name, resp.Status)
	}

	hasher := sha256.New()
	n, err := io.Copy(hasher, io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return "", fmt.Errorf("checksum: hash %s: %w", asset.Name, err)
	}
	if n > maxDownload {
		return "", fmt.Errorf(
			"checksum: asset %q is too large to hash - set %q",
			asset.Name,
			constant.DirectiveSha256URL,
		)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// verify requires the provider digest and the checksums file to agree, so a
// tampered or mismatched checksum fails loud rather than silently pinning one.
func verify(ctx context.Context, client *http.Client, req Request) (string, error) {
	fromDigest, err := digest(req)
	if err != nil {
		return "", fmt.Errorf("checksum: verify: %w", err)
	}
	fromFile, err := checksums(ctx, client, req)
	if err != nil {
		return "", fmt.Errorf("checksum: verify: %w", err)
	}
	if fromDigest != fromFile {
		return "", fmt.Errorf(
			"checksum: digest %s and checksums file %s disagree",
			fromDigest,
			fromFile,
		)
	}
	return fromDigest, nil
}

// matchAsset picks the single asset matching req.Pattern, ignoring checksum and
// signature siblings so a pattern does not also match a .sha256 next to its asset.
func matchAsset(req Request) (model.Asset, error) {
	if req.Pattern == "" {
		return model.Asset{}, fmt.Errorf(
			"checksum: %q is required to pick an asset",
			constant.DirectivePattern,
		)
	}
	p, err := pattern.Compile(req.Pattern)
	if err != nil {
		return model.Asset{}, fmt.Errorf("checksum: invalid pattern %q: %w", req.Pattern, err)
	}

	var matched []model.Asset
	for _, a := range req.Assets {
		if isSidecar(a.Name) {
			continue
		}
		if p.Matches(a.Name) {
			matched = append(matched, a)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return model.Asset{}, fmt.Errorf("checksum: no asset matched pattern %q", req.Pattern)
	default:
		return model.Asset{}, fmt.Errorf(
			"checksum: pattern %q matched %d assets",
			req.Pattern,
			len(matched),
		)
	}
}

// siblingChecksumURL finds a checksum file among assets: a per-asset
// "<name>.sha256" first, else a combined list (checksums.txt, sha256sums.txt).
func siblingChecksumURL(assets []model.Asset, assetName string) string {
	var combined string
	for _, a := range assets {
		switch {
		case a.Name == assetName+".sha256":
			return a.URL
		case combined == "" && isChecksumList(a.Name):
			combined = a.URL
		}
	}
	return combined
}

// hashForFile returns the entry whose file matches name, or the sole bare hash.
func hashForFile(entries []entry, name string) (string, error) {
	for _, e := range entries {
		if e.file == name {
			return e.hash, nil
		}
	}
	if len(entries) == 1 && entries[0].file == "" {
		return entries[0].hash, nil
	}
	return "", fmt.Errorf("checksum: %q not found in the checksum file", name)
}

// sidecarExts are the extensions of checksum and signature files that ride
// alongside release artifacts.
var sidecarExts = []string{
	".sha256",
	".sha512",
	".md5",
	".sum",
	".sig",
	".asc",
	".pem",
	".cert",
	".sbom",
}

// isSidecar reports whether name is a checksum or signature file rather than a
// release artifact to pin.
func isSidecar(name string) bool {
	lower := strings.ToLower(name)
	if isChecksumList(lower) {
		return true
	}
	return slices.ContainsFunc(sidecarExts, func(ext string) bool {
		return strings.HasSuffix(lower, ext)
	})
}

// isChecksumList reports whether name is a combined checksum file.
func isChecksumList(name string) bool {
	return xstrings.ContainsAny(strings.ToLower(name), "checksum", "sha256sum")
}
