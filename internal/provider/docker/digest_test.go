package docker_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/stretchr/testify/require"
)

func TestDigest(t *testing.T) {
	t.Parallel()

	const want = "sha256:b0a73115a4313244422ef5348a3cfa1068a0a189e54c4c3c3e3a41c050d4f96e"

	var manifestHost string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			manifestHost = req.URL.Host
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
	p := docker.New(docker.WithTransport(transport), anon())

	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)
	digest, err := p.Digest(t.Context(), res, "1.27")
	require.NoError(t, err)

	require.Equal(t, want, digest)
	require.Equal(t, "registry-1.docker.io", manifestHost,
		"a Hub digest resolves via the registry, not the web API")
}
