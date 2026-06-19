package docker_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/docker"
	xhttp "github.com/gechr/x/http"
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

func TestHubTokenRetriesAfterTransientLoginFailure(t *testing.T) {
	t.Parallel()

	var logins int
	var tagsAuth []string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/users/login"):
			logins++
			if logins == 1 {
				status := xhttp.Status(http.StatusInternalServerError)
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Status:     status,
					Body:       http.NoBody,
					Request:    req,
				}, nil
			}
			return jsonResponse(req, `{"token":"jwt"}`), nil
		default:
			tagsAuth = append(tagsAuth, req.Header.Get("Authorization"))
			return jsonResponse(req, `{"results":[{"name":"1.0.0"}]}`), nil
		}
	})
	// docker login credentials live under the Hub auth host.
	keychain := hostKeychain{
		host: "index.docker.io",
		cfg:  authn.AuthConfig{Username: "u", Password: "p"},
	}
	p := docker.New(docker.WithTransport(transport), docker.WithKeychain(keychain))

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)

	_, err = p.Discover(t.Context(), res) // login fails transiently, falls back to anonymous
	require.NoError(t, err)
	_, err = p.Discover(t.Context(), res) // retries login, now succeeds
	require.NoError(t, err)

	require.Equal(t, 2, logins, "the transient failure is not cached, so login is retried")
	require.Equal(t, []string{"", "Bearer jwt"}, tagsAuth,
		"first request is anonymous, the retry authenticates with the minted token")
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
			wantErr: "docker: repository is required",
		},
		{
			name:    "whitespace is rejected",
			pairs:   []directive.KV{{Key: "repository", Value: "ng inx"}},
			wantErr: `docker: repository "ng inx" must not contain whitespace`,
		},
		{
			name:    "a host in the repository is rejected",
			pairs:   []directive.KV{{Key: "repository", Value: "ghcr.io://owner/img"}},
			wantErr: `docker: put the registry host in registry, not repository (got "ghcr.io://owner/img")`,
		},
	}

	p := docker.New(anon())
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

func TestStatusErrAddsHintOnAuthFailure(t *testing.T) {
	t.Parallel()

	// A rate-limited tags list surfaces the auth hint, at the failing registry.
	status := xhttp.Status(http.StatusTooManyRequests)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Status:     status,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)
	_, err = p.Discover(t.Context(), res)
	require.EqualError(t, err, "docker: list hub tags: "+status+" ("+docker.AuthHint+")")
}
