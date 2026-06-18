package docker_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/docker"
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

// anon configures a provider with the anonymous keychain.
func anon() docker.Option { return docker.WithKeychain(anonKeychain{}) }

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// jsonResponse builds a 200 JSON response carrying body.
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

func TestNameAndKeys(t *testing.T) {
	t.Parallel()

	p := docker.New(anon())
	require.Equal(t, "docker", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 2)
	require.Equal(t, "repository", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "registry", keys[1].Name)
	require.False(t, keys[1].Required)
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
			name:  "bare image gains the library namespace",
			pairs: []directive.KV{{Key: "repository", Value: "nginx"}},
			want:  "docker.io/library/nginx",
		},
		{
			name:  "namespaced hub image",
			pairs: []directive.KV{{Key: "repository", Value: "grafana/grafana"}},
			want:  "docker.io/grafana/grafana",
		},
		{
			name: "explicit registry",
			pairs: []directive.KV{
				{Key: "repository", Value: "owner/img"},
				{Key: "registry", Value: "ghcr.io"},
			},
			want: "ghcr.io/owner/img",
		},
		{
			name: "docker.io alias routes to the hub",
			pairs: []directive.KV{
				{Key: "repository", Value: "redis"},
				{Key: "registry", Value: "docker.io"},
			},
			want: "docker.io/library/redis",
		},
		{
			name:    "repository is required",
			pairs:   []directive.KV{{Key: "registry", Value: "ghcr.io"}},
			wantErr: "repository is required",
		},
		{
			name:    "whitespace is rejected",
			pairs:   []directive.KV{{Key: "repository", Value: "ng inx"}},
			wantErr: "must not contain whitespace",
		},
		{
			name:    "a host in the repository is rejected",
			pairs:   []directive.KV{{Key: "repository", Value: "ghcr.io://owner/img"}},
			wantErr: "put the registry host in registry",
		},
	}

	p := docker.New(anon())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := p.Resource(directiveOf(tt.pairs...))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, p.Describe(res))
		})
	}
}

func TestAuthenticateAnonymous(t *testing.T) {
	t.Parallel()

	err := docker.New(anon()).Authenticate(t.Context())
	require.ErrorContains(t, err, "anonymous")
}

func TestAuthenticateWithEnvToken(t *testing.T) {
	t.Setenv("CLOVER_DOCKER_TOKEN", "secret")

	require.NoError(t, docker.New(anon()).Authenticate(t.Context()))
}
