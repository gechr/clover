package docker_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/require"
)

// hostKeychain returns credentials only for a specific host.
type hostKeychain struct {
	host string
	cfg  authn.AuthConfig
}

func (k hostKeychain) Resolve(r authn.Resource) (authn.Authenticator, error) {
	if r.RegistryStr() == k.host {
		return authn.FromConfig(k.cfg), nil
	}
	return authn.Anonymous, nil
}

func TestDigestHubResolvesAuthViaIndexHost(t *testing.T) {
	t.Parallel()

	var tokenAuth string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			tokenAuth = req.Header.Get("Authorization")
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			header := http.Header{}
			header.Set("Docker-Content-Digest", "sha256:"+strings.Repeat("a", 64))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})
	// Credentials are keyed under Docker's auth host, where docker login stores them.
	keychain := hostKeychain{
		host: "index.docker.io",
		cfg:  authn.AuthConfig{Username: "u", Password: "p"},
	}
	p := docker.New(docker.WithTransport(transport), docker.WithKeychain(keychain))

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)
	_, err = p.Digest(t.Context(), res, "1.27")
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(tokenAuth, "Basic "),
		"the Hub token exchange resolves credentials via index.docker.io")
}
