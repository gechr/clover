package github_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

// captureTransport answers every request with a fixed body, recording the
// request URL
// and the headers DownloadAsset is expected to set.
func captureTransport(url, auth, accept, cache *string) roundTripFunc {
	return func(req *http.Request) (*http.Response, error) {
		*url = req.URL.String()
		*auth = req.Header.Get("Authorization")
		*accept = req.Header.Get("Accept")
		*cache = req.Header.Get("Cache-Control")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("bits")),
			Request:    req,
		}, nil
	}
}

// releaseAsset is the fixture asset every download test fetches.
var releaseAsset = model.Asset{
	Name:   "tool.tar.gz",
	URL:    "https://github.com/owner/name/releases/download/v1.0.0/tool.tar.gz",
	APIURL: "https://api.github.com/repos/owner/name/releases/assets/1",
}

func TestDownloadAssetAuthenticatedUsesAPIURL(t *testing.T) {
	t.Parallel()

	var url, auth, accept, cache string
	p := github.New(
		github.WithTransport(captureTransport(&url, &auth, &accept, &cache)),
		github.WithToken("tok"),
	)
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	body, err := p.DownloadAsset(t.Context(), res, releaseAsset)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "bits", string(data))
	require.Equal(t, releaseAsset.APIURL, url,
		"a credentialed download reads the asset through its API endpoint")
	require.Equal(t, "Bearer tok", auth)
	require.Equal(t, "application/octet-stream", accept)
	require.Equal(t, "no-store", cache, "asset bytes stay out of the HTTP cache")
}

func TestDownloadAssetAnonymousUsesBrowserURL(t *testing.T) {
	t.Parallel()

	var url, auth, accept, cache string
	p := github.New(github.WithTransport(captureTransport(&url, &auth, &accept, &cache)))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	body, err := p.DownloadAsset(t.Context(), res, releaseAsset)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, releaseAsset.URL, url,
		"without a credential the public browser URL is fetched directly")
	require.Empty(t, auth)
}

func TestDownloadAssetForeignAPIURLGetsNoToken(t *testing.T) {
	t.Parallel()

	var url, auth, accept, cache string
	p := github.New(
		github.WithTransport(captureTransport(&url, &auth, &accept, &cache)),
		github.WithToken("tok"),
	)
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	asset := releaseAsset
	asset.APIURL = "https://evil.example.com/repos/owner/name/releases/assets/1"
	body, err := p.DownloadAsset(t.Context(), res, asset)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, asset.URL, url,
		"an API URL off the host's API origin is ignored, so the token cannot be redirected")
	require.Empty(t, auth)
}

func TestDownloadAssetNonOKStatus(t *testing.T) {
	t.Parallel()

	p := github.New(github.WithTransport(roundTripFunc(
		func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		},
	)))
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	_, err = p.DownloadAsset(t.Context(), res, releaseAsset)
	require.EqualError(t, err, "github: download tool.tar.gz: 404 Not Found")
}

func TestDownloadAssetInvalidResource(t *testing.T) {
	t.Parallel()

	p := github.New(github.WithTransport(roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			t.Error("unexpected HTTP request")
			return nil, errors.New("no request expected")
		},
	)))
	_, err := p.DownloadAsset(t.Context(), "bogus", releaseAsset)
	require.EqualError(t, err, "github: invalid resource string")
}

func TestDownloadAssetDropsTokenOnCDNRedirect(t *testing.T) {
	t.Parallel()

	auths := make(map[string]string)
	p := github.New(
		github.WithTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
			auths[req.URL.Host] = req.Header.Get("Authorization")
			if req.URL.Host == "api.github.com" {
				return &http.Response{
					StatusCode: http.StatusFound,
					Header: http.Header{
						"Location": {"https://objects.example.com/blob/1"},
					},
					Body:    io.NopCloser(strings.NewReader("")),
					Request: req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("bits")),
				Request:    req,
			}, nil
		})),
		github.WithToken("tok"),
	)
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "owner/name"}))
	require.NoError(t, err)

	body, err := p.DownloadAsset(t.Context(), res, releaseAsset)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "Bearer tok", auths["api.github.com"])
	require.Empty(t, auths["objects.example.com"],
		"the bearer must not follow the redirect to the CDN")
}

func TestDownloadAssetGHESUsesAPIURL(t *testing.T) {
	t.Parallel()

	var url, auth, accept, cache string
	p := github.New(
		github.WithTransport(captureTransport(&url, &auth, &accept, &cache)),
		github.WithStore(stubStore{token: "ghe-tok", ok: true}),
	)
	res, err := p.Resource(directiveOf(
		directive.KV{Key: "repository", Value: "owner/name"},
		directive.KV{Key: "host", Value: "ghe.example.com"},
	))
	require.NoError(t, err)

	asset := model.Asset{
		Name:   "tool.tar.gz",
		URL:    "https://ghe.example.com/owner/name/releases/download/v1.0.0/tool.tar.gz",
		APIURL: "https://ghe.example.com/api/v3/repos/owner/name/releases/assets/1",
	}
	body, err := p.DownloadAsset(t.Context(), res, asset)
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, asset.APIURL, url,
		"a GHES credential downloads through the instance's /api/v3 origin")
	require.Equal(t, "Bearer ghe-tok", auth)
}
