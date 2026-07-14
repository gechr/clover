package gitlab_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/gitlab"
	"github.com/stretchr/testify/require"
)

// captureTransport records the download request's URL, auth, and cache headers,
// answering with a small body.
func captureTransport(gotURL, gotAuth, gotCache *string) roundTripFunc {
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

func downloadAsset(
	t *testing.T,
	p *gitlab.Provider,
	asset model.Asset,
) (io.ReadCloser, error) {
	t.Helper()
	return p.DownloadAsset(
		t.Context(),
		resourceFor(t, p, directive.KV{Key: "repository", Value: "group/proj"}),
		asset,
	)
}

// TestDownloadAssetAuthenticated covers the credentialed path: the api/v4 link
// is fetched with the bearer and a no-store cache directive.
func TestDownloadAssetAuthenticated(t *testing.T) {
	t.Parallel()

	var gotURL, gotAuth, gotCache string
	p := gitlab.New(
		gitlab.WithTransport(captureTransport(&gotURL, &gotAuth, &gotCache)),
		gitlab.WithToken("tok"),
	)

	const api = "https://gitlab.com/api/v4/projects/group%2Fproj/packages/generic/tool/1.0.0/x.tgz"
	body, err := downloadAsset(t, p, model.Asset{
		Name:   "x.tgz",
		URL:    "https://gitlab.com/group/proj/-/releases/v1.0.0/downloads/x.tgz",
		APIURL: api,
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, api, gotURL)
	require.Equal(t, "Bearer tok", gotAuth)
	require.Equal(t, "no-store", gotCache)
}

// TestDownloadAssetAnonymous covers the uncredentialed path: the browser URL is
// fetched with no Authorization header.
func TestDownloadAssetAnonymous(t *testing.T) {
	t.Parallel()

	var gotURL, gotAuth, gotCache string
	p := gitlab.New(gitlab.WithTransport(captureTransport(&gotURL, &gotAuth, &gotCache)))

	body, err := downloadAsset(t, p, model.Asset{
		Name:   "x.tgz",
		URL:    "https://gitlab.com/group/proj/-/releases/v1.0.0/downloads/x.tgz",
		APIURL: "https://gitlab.com/api/v4/projects/group%2Fproj/packages/generic/tool/1.0.0/x.tgz",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "https://gitlab.com/group/proj/-/releases/v1.0.0/downloads/x.tgz", gotURL)
	require.Empty(t, gotAuth)
}

// TestDownloadAssetForeignAPIURLGetsNoToken covers the exfil guard: a release
// link pointing off the host's API origin is fetched via the browser URL with
// no token.
func TestDownloadAssetForeignAPIURLGetsNoToken(t *testing.T) {
	t.Parallel()

	var gotURL, gotAuth, gotCache string
	p := gitlab.New(
		gitlab.WithTransport(captureTransport(&gotURL, &gotAuth, &gotCache)),
		gitlab.WithToken("tok"),
	)

	body, err := downloadAsset(t, p, model.Asset{
		Name:   "x.tgz",
		URL:    "https://external.example.com/x.tgz",
		APIURL: "https://evil.example.com/api/v4/steal",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "https://external.example.com/x.tgz", gotURL)
	require.Empty(t, gotAuth)
}

// TestDownloadAssetDropsTokenOnRedirect covers the object-storage hop: the
// bearer reaches the API host, and Go's client strips it on the cross-host
// redirect.
func TestDownloadAssetDropsTokenOnRedirect(t *testing.T) {
	t.Parallel()

	auths := map[string]string{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auths[req.URL.Host] = req.Header.Get("Authorization")
		if req.URL.Host == "gitlab.com" {
			header := http.Header{}
			header.Set("Location", "https://objects.example.com/x.tgz")
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
	p := gitlab.New(gitlab.WithTransport(transport), gitlab.WithToken("tok"))

	body, err := downloadAsset(t, p, model.Asset{
		Name:   "x.tgz",
		URL:    "https://gitlab.com/group/proj/-/releases/v1.0.0/downloads/x.tgz",
		APIURL: "https://gitlab.com/api/v4/projects/group%2Fproj/packages/generic/tool/1.0.0/x.tgz",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "Bearer tok", auths["gitlab.com"])
	require.Empty(t, auths["objects.example.com"], "the bearer must not follow the redirect")
}

// TestDownloadAssetDropsTokenOnSubdomainRedirect covers the hop Go's default
// policy would forward the header on: a redirect to a subdomain of the forge
// host leaves the original origin, so the bearer is dropped.
func TestDownloadAssetDropsTokenOnSubdomainRedirect(t *testing.T) {
	t.Parallel()

	auths := map[string]string{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auths[req.URL.Scheme+"://"+req.URL.Host] = req.Header.Get("Authorization")
		switch {
		case req.URL.Host == "gitlab.com" && req.URL.Scheme == "https":
			header := http.Header{}
			header.Set("Location", "https://assets.gitlab.com/x.tgz")
			return &http.Response{
				StatusCode: http.StatusFound,
				Header:     header,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		default:
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("bytes")),
				Request:    req,
			}, nil
		}
	})
	p := gitlab.New(gitlab.WithTransport(transport), gitlab.WithToken("tok"))

	body, err := downloadAsset(t, p, model.Asset{
		Name:   "x.tgz",
		URL:    "https://gitlab.com/group/proj/-/releases/v1.0.0/downloads/x.tgz",
		APIURL: "https://gitlab.com/api/v4/projects/group%2Fproj/packages/generic/tool/1.0.0/x.tgz",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "Bearer tok", auths["https://gitlab.com"])
	require.Empty(t, auths["https://assets.gitlab.com"],
		"the bearer must not follow a subdomain redirect")
}

// TestDownloadAssetDropsTokenOnSchemeDowngrade covers the same-host https->http
// hop, which Go's default policy would also forward the header on.
func TestDownloadAssetDropsTokenOnSchemeDowngrade(t *testing.T) {
	t.Parallel()

	auths := map[string]string{}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auths[req.URL.Scheme+"://"+req.URL.Host] = req.Header.Get("Authorization")
		if req.URL.Scheme == "https" {
			header := http.Header{}
			header.Set("Location", "http://gitlab.com/x.tgz")
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
	p := gitlab.New(gitlab.WithTransport(transport), gitlab.WithToken("tok"))

	body, err := downloadAsset(t, p, model.Asset{
		Name:   "x.tgz",
		URL:    "https://gitlab.com/group/proj/-/releases/v1.0.0/downloads/x.tgz",
		APIURL: "https://gitlab.com/api/v4/projects/group%2Fproj/packages/generic/tool/1.0.0/x.tgz",
	})
	require.NoError(t, err)
	t.Cleanup(func() { body.Close() })

	require.Equal(t, "Bearer tok", auths["https://gitlab.com"])
	require.Empty(t, auths["http://gitlab.com"],
		"the bearer must not survive a scheme downgrade")
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
	p := gitlab.New(gitlab.WithTransport(transport))

	_, err := downloadAsset(t, p, model.Asset{Name: "x.tgz", URL: "https://gitlab.com/x.tgz"})
	require.EqualError(t, err, "gitlab: download x.tgz: 404 Not Found")
}

func TestDownloadAssetInvalidResource(t *testing.T) {
	t.Parallel()

	_, err := gitlab.New().DownloadAsset(t.Context(), "not-a-resource", model.Asset{})
	require.EqualError(t, err, "gitlab: invalid resource string")
}
