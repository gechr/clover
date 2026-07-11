package oci_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/oci"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/require"
)

// errKeychain fails to resolve, standing in for a broken credential store.
type errKeychain struct{ err error }

func (k errKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return nil, k.err
}

// staticAuth returns a fixed authorization config.
type staticAuth struct {
	cfg *authn.AuthConfig
	err error
}

func (a staticAuth) Authorization() (*authn.AuthConfig, error) { return a.cfg, a.err }

// staticKeychain resolves to a fixed authenticator, asserting it is queried for
// the expected registry host (exercising the registryResource adapter).
type staticKeychain struct {
	t    *testing.T
	host string
	auth authn.Authenticator
}

func (k staticKeychain) Resolve(res authn.Resource) (authn.Authenticator, error) {
	require.Equal(k.t, k.host, res.RegistryStr())
	require.Equal(k.t, k.host, res.String())
	return k.auth, nil
}

func TestResolveAuthTokenEnv(t *testing.T) {
	const env = "CLOVER_TEST_OCI_TOKEN"
	t.Setenv(env, "ready-token")

	client := oci.New(oci.WithTokenEnv(env), oci.WithKeychain(anonKeychain{}))
	cfg := client.ResolveAuth("registry.example.com")
	require.NotNil(t, cfg)
	require.Equal(t, &authn.AuthConfig{RegistryToken: "ready-token"}, cfg)
}

func TestResolveAuthKeychainError(t *testing.T) {
	t.Parallel()

	client := oci.New(oci.WithKeychain(errKeychain{err: fmt.Errorf("no store")}))
	require.Nil(t, client.ResolveAuth("registry.example.com"))
}

func TestResolveAuthEmptyConfig(t *testing.T) {
	t.Parallel()

	const host = "registry.example.com"
	client := oci.New(oci.WithKeychain(staticKeychain{
		t:    t,
		host: host,
		auth: staticAuth{cfg: &authn.AuthConfig{}},
	}))
	require.Nil(t, client.ResolveAuth(host), "an empty config is anonymous access")
}

func TestResolveAuthPopulatedConfig(t *testing.T) {
	t.Parallel()

	const host = "registry.example.com"
	want := &authn.AuthConfig{Username: "u", Password: "p"}
	client := oci.New(oci.WithKeychain(staticKeychain{
		t:    t,
		host: host,
		auth: staticAuth{cfg: want},
	}))
	require.Equal(t, want, client.ResolveAuth(host))
}

func TestHTTPClientBuildsDefaultStack(t *testing.T) {
	t.Parallel()

	// With no transport override, the lazily-built cache and rate-limit stack is
	// still a usable client, built without issuing any request.
	require.NotNil(t, oci.New().HTTPClient())
}

func TestTagsUsesAuthHost(t *testing.T) {
	t.Parallel()

	var tokenService string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			tokenService = req.URL.Query().Get("service")
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return jsonResponse(req, `{"tags":["1.0.0"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	tags, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{
			Host:       "registry-1.docker.io",
			AuthHost:   "index.docker.io",
			Repository: "library/nginx",
		},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, tags)
	require.Equal(t, "example.com", tokenService, "the challenge drives the token exchange")
}
