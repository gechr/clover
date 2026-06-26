package helm_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/helm"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

// anonKeychain always resolves to anonymous access, so tests never read the
// developer's real docker login.
type anonKeychain struct{}

func (anonKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

func anon() helm.Option { return helm.WithKeychain(anonKeychain{}) }

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func bodyResponse(req *http.Request, body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
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
		`Bearer realm="https://auth.example.com/token",service="example.com",scope="repository:charts/app:pull"`,
	)
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}
}

func TestNameAndKeys(t *testing.T) {
	t.Parallel()

	p := helm.New(anon())
	require.Equal(t, "helm", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 2)
	require.Equal(t, "registry", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "chart", keys[1].Name)
	require.True(t, keys[1].Required)
}

func TestResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pairs   []directive.KV
		want    string
		wantErr string
	}{
		{
			name: "classic https repository",
			pairs: []directive.KV{
				{Key: "registry", Value: "https://charts.bitnami.com/bitnami"},
				{Key: "chart", Value: "nginx"},
			},
			want: "charts.bitnami.com/bitnami/nginx",
		},
		{
			name: "oci registry",
			pairs: []directive.KV{
				{Key: "registry", Value: "oci://registry-1.docker.io/bitnamicharts"},
				{Key: "chart", Value: "nginx"},
			},
			want: "oci://registry-1.docker.io/bitnamicharts/nginx",
		},
		{
			name:    "registry is required",
			pairs:   []directive.KV{{Key: "chart", Value: "nginx"}},
			wantErr: "helm: registry is required",
		},
		{
			name:    "chart is required",
			pairs:   []directive.KV{{Key: "registry", Value: "https://charts.example.com"}},
			wantErr: "helm: chart is required",
		},
		{
			name: "chart with a slash is rejected",
			pairs: []directive.KV{
				{Key: "registry", Value: "https://charts.example.com"},
				{Key: "chart", Value: "team/nginx"},
			},
			wantErr: `helm: put the repository path in registry, not chart (got "team/nginx")`,
		},
		{
			name: "registry without a scheme is rejected",
			pairs: []directive.KV{
				{Key: "registry", Value: "charts.example.com"},
				{Key: "chart", Value: "nginx"},
			},
			wantErr: `helm: registry "charts.example.com" must start with https://, http:// or oci://`,
		},
		{
			name: "unsupported scheme is rejected",
			pairs: []directive.KV{
				{Key: "registry", Value: "ftp://charts.example.com"},
				{Key: "chart", Value: "nginx"},
			},
			wantErr: `helm: registry "ftp://charts.example.com" has an unsupported scheme "ftp" (use https://, http:// or oci://)`,
		},
	}

	p := helm.New(anon())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := p.Resource(directiveOf(tt.pairs...))
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, p.Describe(res))
		})
	}
}

const indexYAML = `apiVersion: v1
entries:
  nginx:
    - version: 18.0.0
      created: "2026-01-02T03:04:05Z"
      digest: aaaa1111
      urls:
        - https://charts.bitnami.com/bitnami/nginx-18.0.0.tgz
    - version: 18.1.0
      created: "2026-02-02T03:04:05Z"
      digest: bbbb2222
      urls:
        - nginx-18.1.0.tgz
`

func TestDiscoverIndex(t *testing.T) {
	t.Parallel()

	var path string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		path = req.URL.Path
		return bodyResponse(req, indexYAML), nil
	})
	p := helm.New(helm.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(
		directive.KV{Key: "registry", Value: "https://charts.bitnami.com/bitnami"},
		directive.KV{Key: "chart", Value: "nginx"},
	))
	require.NoError(t, err)
	candidates, err := p.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Equal(t, "/bitnami/index.yaml", path)
	require.Len(t, candidates, 2)

	require.Equal(t, "18.0.0", candidates[0].Version)
	require.NotNil(t, candidates[0].Semver)
	require.False(t, candidates[0].PublishedAt.IsZero(), "the index supplies release dates")
	require.Len(t, candidates[0].Assets, 1)
	require.Equal(t, "sha256:aaaa1111", candidates[0].Assets[0].Digest,
		"a bare hex digest gains the sha256: prefix")
	require.Equal(t, "nginx-18.0.0.tgz", candidates[0].Assets[0].Name)
	require.Equal(
		t,
		"https://charts.bitnami.com/bitnami/nginx-18.0.0.tgz",
		candidates[0].Assets[0].URL,
	)

	require.Equal(
		t,
		"https://charts.bitnami.com/bitnami/nginx-18.1.0.tgz",
		candidates[1].Assets[0].URL,
		"a relative index URL resolves against the repository base",
	)
}

func TestDiscoverIndexChartNotFound(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return bodyResponse(req, indexYAML), nil
	})
	p := helm.New(helm.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(
		directive.KV{Key: "registry", Value: "https://charts.bitnami.com/bitnami"},
		directive.KV{Key: "chart", Value: "redis"},
	))
	require.NoError(t, err)
	_, err = p.Discover(t.Context(), res)
	require.EqualError(
		t,
		err,
		`helm: chart "redis" not found in https://charts.bitnami.com/bitnami`,
	)
}

func TestDiscoverRegistryBearerChallenge(t *testing.T) {
	t.Parallel()

	var gotScope string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			gotScope = req.URL.Query().Get("scope")
			return bodyResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			require.Contains(t, req.URL.Path, "/v2/charts/app/tags/list")
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			require.Equal(t, "Bearer abc", req.Header.Get("Authorization"))
			return bodyResponse(req, `{"tags":["1.0.0","1.1.0","latest"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})
	p := helm.New(helm.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(
		directive.KV{Key: "registry", Value: "oci://registry.example.com/charts"},
		directive.KV{Key: "chart", Value: "app"},
	))
	require.NoError(t, err)
	candidates, err := p.Discover(t.Context(), res)
	require.NoError(t, err)

	require.Equal(t, "repository:charts/app:pull", gotScope)
	require.Len(t, candidates, 3)
	require.Equal(t, "1.0.0", candidates[0].Version)
	require.NotNil(t, candidates[0].Semver)
	require.True(t, candidates[0].PublishedAt.IsZero(), "registry tags carry no dates")
	require.Nil(t, candidates[2].Semver, "a non-semver tag yields a nil Semver")
}

func TestDigest(t *testing.T) {
	t.Parallel()

	const want = "sha256:b0a73115a4313244422ef5348a3cfa1068a0a189e54c4c3c3e3a41c050d4f96e"

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return bodyResponse(req, `{"token":"abc"}`), nil
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
	p := helm.New(helm.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(
		directive.KV{Key: "registry", Value: "oci://registry.example.com/charts"},
		directive.KV{Key: "chart", Value: "app"},
	))
	require.NoError(t, err)
	digest, err := p.Digest(t.Context(), res, "1.1.0")
	require.NoError(t, err)
	require.Equal(t, want, digest)
}

func TestDigestClassicErrors(t *testing.T) {
	t.Parallel()

	p := helm.New(anon())
	res, err := p.Resource(directiveOf(
		directive.KV{Key: "registry", Value: "https://charts.bitnami.com/bitnami"},
		directive.KV{Key: "chart", Value: "nginx"},
	))
	require.NoError(t, err)
	_, err = p.Digest(t.Context(), res, "18.0.0")
	require.EqualError(t, err,
		"helm: digest is only available for oci:// charts, not https://charts.bitnami.com/bitnami")
}
