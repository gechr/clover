package gitea_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/gitea"
	"github.com/stretchr/testify/require"
)

// captureDownload records the download request's URL, auth, and cache headers,
// answering with a small body.
func captureDownload(gotURL, gotAuth, gotCache *string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		*gotURL = req.URL.String()
		*gotAuth = req.Header.Get("Authorization")
		*gotCache = req.Header.Get("Cache-Control")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("bytes")),
			Request:    req,
		}, nil
	}
}

// TestDownloadAssetPAT covers the PAT path: a first-party attachment download
// carries the credential in Gitea's `token` scheme, with a no-store directive.
func TestDownloadAssetPAT(t *testing.T) {
	t.Parallel()

	var gotURL, gotAuth, gotCache string
	p := gitea.New(
		gitea.WithTransport(captureDownload(&gotURL, &gotAuth, &gotCache)),
		gitea.WithToken("tok"),
	)

	const url = "https://codeberg.org/attachments/aaaa-bbbb"
	body, err := p.DownloadAsset(t.Context(), resourceFor(t, p), model.Asset{
		Name: "x.tgz",
		URL:  url,
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, url, gotURL)
	require.Equal(t, "token tok", gotAuth, "a PAT goes as `token`, not `Bearer`")
	require.Equal(t, "no-store", gotCache)
}

// TestDownloadAssetOAuth covers the minted-login path: the stored access token
// goes as `Bearer`.
func TestDownloadAssetOAuth(t *testing.T) {
	t.Parallel()

	blob, err := json.Marshal(map[string]string{"access_token": "minted"})
	require.NoError(t, err)
	store := &memStore{m: map[string]string{"codeberg.org": string(blob)}}

	var gotURL, gotAuth, gotCache string
	p := gitea.New(
		gitea.WithTransport(captureDownload(&gotURL, &gotAuth, &gotCache)),
		gitea.WithStore(store),
	)

	body, err := p.DownloadAsset(t.Context(), resourceFor(t, p), model.Asset{
		Name: "x.tgz",
		URL:  "https://codeberg.org/attachments/aaaa-bbbb",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "Bearer minted", gotAuth)
}

// TestDownloadAssetAnonymous covers the uncredentialed path: no Authorization
// header is sent.
func TestDownloadAssetAnonymous(t *testing.T) {
	t.Parallel()

	var gotURL, gotAuth, gotCache string
	p := gitea.New(gitea.WithTransport(captureDownload(&gotURL, &gotAuth, &gotCache)))

	body, err := p.DownloadAsset(t.Context(), resourceFor(t, p), model.Asset{
		Name: "x.tgz",
		URL:  "https://codeberg.org/attachments/aaaa-bbbb",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Empty(t, gotAuth)
}

// TestDownloadAssetExternalGetsNoToken covers the exfil guard: an external-type
// asset points off the forge host and must not receive the credential.
func TestDownloadAssetExternalGetsNoToken(t *testing.T) {
	t.Parallel()

	var gotURL, gotAuth, gotCache string
	p := gitea.New(
		gitea.WithTransport(captureDownload(&gotURL, &gotAuth, &gotCache)),
		gitea.WithToken("tok"),
	)

	body, err := p.DownloadAsset(t.Context(), resourceFor(t, p), model.Asset{
		Name: "x.tgz",
		URL:  "https://external.example.com/x.tgz",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "https://external.example.com/x.tgz", gotURL)
	require.Empty(t, gotAuth)
}

// TestDownloadAssetDropsTokenOnRedirect covers the object-storage hop: the
// credential reaches the forge host, and Go's client strips it on the
// cross-host redirect.
func TestDownloadAssetDropsTokenOnRedirect(t *testing.T) {
	t.Parallel()

	auths := map[string]string{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auths[req.URL.Host] = req.Header.Get("Authorization")
		if req.URL.Host == "codeberg.org" {
			header := http.Header{}
			header.Set("Location", "https://storage.example.com/x.tgz")
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     header,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("bytes")),
			Request:    req,
		}, nil
	})
	p := gitea.New(gitea.WithTransport(transport), gitea.WithToken("tok"))

	body, err := p.DownloadAsset(t.Context(), resourceFor(t, p), model.Asset{
		Name: "x.tgz",
		URL:  "https://codeberg.org/attachments/aaaa-bbbb",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "token tok", auths["codeberg.org"])
	require.Empty(t, auths["storage.example.com"], "the credential must not follow the redirect")
}

// TestDownloadAssetDropsTokenOnSubdomainRedirect covers the hop Go's default
// policy would forward the header on: a redirect to a subdomain of the forge
// host leaves the original origin, so the credential is dropped.
func TestDownloadAssetDropsTokenOnSubdomainRedirect(t *testing.T) {
	t.Parallel()

	auths := map[string]string{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auths[req.URL.Host] = req.Header.Get("Authorization")
		if req.URL.Host == "codeberg.org" {
			header := http.Header{}
			header.Set("Location", "https://storage.codeberg.org/x.tgz")
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     header,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("bytes")),
			Request:    req,
		}, nil
	})
	p := gitea.New(gitea.WithTransport(transport), gitea.WithToken("tok"))

	body, err := p.DownloadAsset(t.Context(), resourceFor(t, p), model.Asset{
		Name: "x.tgz",
		URL:  "https://codeberg.org/attachments/aaaa-bbbb",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "token tok", auths["codeberg.org"])
	require.Empty(t, auths["storage.codeberg.org"],
		"the credential must not follow a subdomain redirect")
}

func TestDownloadAssetError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Header:     http.Header{},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})
	p := gitea.New(gitea.WithTransport(transport))

	_, err := p.DownloadAsset(t.Context(), resourceFor(t, p), model.Asset{
		Name: "x.tgz",
		URL:  "https://codeberg.org/attachments/aaaa-bbbb",
	})
	require.EqualError(t, err, "gitea: download x.tgz: 404 Not Found")
}

func TestDownloadAssetInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := gitea.New().DownloadAsset(t.Context(), "not-a-resource", model.Asset{})
	require.EqualError(t, err, "gitea: invalid resource string")
}
