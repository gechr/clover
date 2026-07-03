package oci_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/oci"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/require"
)

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// anonKeychain always resolves to anonymous access, so tests never read the
// developer's real docker login.
type anonKeychain struct{}

func (anonKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

func newClient(rt http.RoundTripper) *oci.Client {
	return oci.New(
		oci.WithTransport(rt),
		oci.WithKeychain(anonKeychain{}),
		oci.WithErrorContext("oci", "authenticate for higher limits"),
	)
}

func jsonResponse(req *http.Request, body string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// challengeResponse is a 401 advertising a bearer-token realm, the way a
// registry answers an unauthenticated request.
func challengeResponse(req *http.Request) *http.Response {
	header := http.Header{}
	header.Set(
		"WWW-Authenticate",
		`Bearer realm="https://auth.example.com/token",service="example.com",scope="repository:team/img:pull,push"`,
	)
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}
}

func TestParseChallenge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		header    string
		wantRealm string
		wantScope string
	}{
		{
			name:      "realm service and scope",
			header:    `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`,
			wantRealm: "https://auth.docker.io/token",
			wantScope: "repository:library/nginx:pull",
		},
		{
			name:      "scope with an embedded comma stays intact",
			header:    `Bearer realm="https://ghcr.io/token",scope="repository:owner/img:pull,push"`,
			wantRealm: "https://ghcr.io/token",
			wantScope: "repository:owner/img:pull,push",
		},
		{
			name:   "a non-bearer scheme yields no realm",
			header: `Basic realm="https://example.com"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			realm, params := oci.ParseChallenge(tt.header)
			require.Equal(t, tt.wantRealm, realm)
			require.Equal(t, tt.wantScope, params["scope"])
		})
	}
}

func TestNextLink(t *testing.T) {
	t.Parallel()

	const base = "https://registry.example.com/v2/team/api/tags/list?n=100"

	link := func(value string) http.Header {
		h := http.Header{}
		if value != "" {
			h.Set("Link", value)
		}
		return h
	}

	// A relative next target resolves against the request URL.
	require.Equal(t,
		"https://registry.example.com/v2/team/api/tags/list?last=z",
		oci.NextLink(link(`</v2/team/api/tags/list?last=z>; rel="next"`), base))

	// A cross-host absolute target is rejected (SSRF guard).
	require.Empty(t,
		oci.NextLink(link(`<http://169.254.169.254/latest/meta-data/>; rel="next"`), base))

	// A non-next relation yields nothing.
	require.Empty(t, oci.NextLink(link(`</v2/team/api/tags/list?last=z>; rel="prev"`), base))

	// No header, no next page.
	require.Empty(t, oci.NextLink(link(""), base))
}

func TestTagsBearerChallenge(t *testing.T) {
	t.Parallel()

	var gotScope string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			gotScope = req.URL.Query().Get("scope")
			require.Equal(t, "example.com", req.URL.Query().Get("service"))
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			require.Equal(t, "Bearer abc", req.Header.Get("Authorization"))
			return jsonResponse(req, `{"tags":["1.0.0","1.2.0","latest"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	tags, truncated, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, "repository:team/img:pull,push", gotScope,
		"the comma-bearing scope from the challenge survives parsing")
	require.Equal(t, []string{"1.0.0", "1.2.0", "latest"}, tags)
	require.False(t, truncated, "a single page with no Link is complete")
}

func TestTagsDeepPaginates(t *testing.T) {
	t.Parallel()

	var calls int
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			calls++
			if req.URL.Query().Get("last") == "" {
				resp := jsonResponse(req, `{"tags":["1.0.0","1.1.0"]}`)
				resp.Header.Set("Link", `</v2/team/img/tags/list?n=100&last=1.1.0>; rel="next"`)
				return resp, nil
			}
			return jsonResponse(req, `{"tags":["1.2.0"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	tags, truncated, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		true,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0", "1.1.0", "1.2.0"}, tags)
	require.Equal(t, 2, calls, "a deep lookup follows the Link header to the next page")
	require.False(t, truncated, "a deep lookup exhausts the pages, so nothing is truncated")
}

func TestTagsShallowReportsTruncation(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			resp := jsonResponse(req, `{"tags":["1.0.0","1.1.0"]}`)
			resp.Header.Set("Link", `</v2/team/img/tags/list?n=100&last=1.1.0>; rel="next"`)
			return resp, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	tags, truncated, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0", "1.1.0"}, tags)
	require.True(t, truncated, "a shallow lookup with a further page reports truncation")
}

func TestDigest(t *testing.T) {
	t.Parallel()

	const want = "sha256:b0a73115a4313244422ef5348a3cfa1068a0a189e54c4c3c3e3a41c050d4f96e"

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			header := http.Header{}
			header.Set("Docker-Content-Digest", want)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	digest, err := newClient(transport).Digest(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		"1.27",
	)
	require.NoError(t, err)
	require.Equal(t, want, digest)
}

func TestTokenReusedAcrossTagsAndDigest(t *testing.T) {
	t.Parallel()

	const want = "sha256:b0a73115a4313244422ef5348a3cfa1068a0a189e54c4c3c3e3a41c050d4f96e"
	var (
		challenges int
		tokens     int
	)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			tokens++
			return jsonResponse(req, `{"token":"abc","expires_in":3600}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				challenges++
				return challengeResponse(req), nil
			}
			require.Equal(t, "Bearer abc", req.Header.Get("Authorization"))
			return jsonResponse(req, `{"tags":["1.0.0"]}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			require.Equal(t, "Bearer abc", req.Header.Get("Authorization"))
			header := http.Header{}
			header.Set("Docker-Content-Digest", want)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})
	client := newClient(transport)
	repo := oci.Repo{Host: "registry.example.com", Repository: "team/img"}

	tags, truncated, err := client.Tags(t.Context(), repo, false)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, tags)
	require.False(t, truncated)

	digest, err := client.Digest(t.Context(), repo, "1.0.0")
	require.NoError(t, err)
	require.Equal(t, want, digest)
	require.Equal(t, 1, challenges)
	require.Equal(t, 1, tokens)
}

const (
	amdDigest = "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	armDigest = "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// indexTransport serves a multi-arch image index for a tag, behind a bearer
// challenge, so a platform lookup reads the per-arch manifest digests.
func indexTransport() roundTripFunc {
	const index = `{"manifests":[` +
		`{"digest":"` + amdDigest + `","platform":{"architecture":"amd64","os":"linux"}},` +
		`{"digest":"` + armDigest + `","platform":{"architecture":"arm64","os":"linux"}}` +
		`]}`
	return func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return jsonResponse(req, index), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	}
}

func TestDigestPlatformSelectsArch(t *testing.T) {
	t.Parallel()

	digest, err := newClient(indexTransport()).Digest(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img", Platform: "linux/arm64"},
		"1.27",
	)
	require.NoError(t, err)
	require.Equal(t, armDigest, digest, "the arm64 manifest digest, not the index digest")
}

func TestDigestPlatformNotFound(t *testing.T) {
	t.Parallel()

	_, err := newClient(indexTransport()).Digest(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img", Platform: "linux/s390x"},
		"1.27",
	)
	require.EqualError(t, err, "oci: no manifest for platform linux/s390x in 1.27")
}

// A single-arch image is not an index, so a platform lookup falls back to the
// sole digest in the Docker-Content-Digest header.
func TestDigestPlatformSingleArch(t *testing.T) {
	t.Parallel()

	const want = "sha256:" + "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			resp := jsonResponse(req, `{"schemaVersion":2,"config":{},"layers":[]}`)
			resp.Header.Set("Docker-Content-Digest", want)
			return resp, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	digest, err := newClient(transport).Digest(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img", Platform: "linux/amd64"},
		"1.27",
	)
	require.NoError(t, err)
	require.Equal(t, want, digest)
}
