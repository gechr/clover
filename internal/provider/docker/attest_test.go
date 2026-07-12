package docker_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/attest"
	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/docker"
	"github.com/sigstore/sigstore-go/pkg/testing/data"
	"github.com/stretchr/testify/require"
)

func TestVerifyAttestation(t *testing.T) {
	t.Parallel()

	const digest = "sha512:46d4e2f74c4877316640000a6fdf8a8b59f1e0847667973e9859f774dd31b8f1e0937813b777fb66a2ac67d50540fe34640966eee9fc2ccca387082b4c85cd3c"
	entity := data.Bundle(t, "sigstore.js@2.0.0-provenance.sigstore.json")
	contents, err := json.Marshal(entity)
	require.NoError(t, err)

	var hosts []string
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		switch req.URL.Path {
		case "/v2/library/nginx/referrers/" + digest:
			return jsonResponse(
				req,
				`{"manifests":[{"artifactType":"application/vnd.dev.sigstore.bundle.v0.3+json","digest":"sha256:manifest"}]}`,
			), nil
		case "/v2/library/nginx/manifests/sha256:manifest":
			return jsonResponse(
				req,
				`{"layers":[{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","digest":"sha256:bundle"}]}`,
			), nil
		case "/v2/library/nginx/blobs/sha256:bundle":
			return jsonResponse(req, string(contents)), nil
		default:
			return nil, fmt.Errorf("no route for %s", req.URL)
		}
	})
	p := docker.New(
		docker.WithTransport(transport),
		docker.WithAttestor(attest.New(attest.WithTrustedMaterial(
			data.TrustedRoot(t, "public-good.json"),
		))),
		anon(),
	)
	res, err := p.Resource(directiveOf(directive.KV{Key: "repository", Value: "nginx"}))
	require.NoError(t, err)

	policy := provider.AttestationPolicy{
		Identity: "https://github.com/sigstore/sigstore-js/.github/workflows/*",
	}
	verified, err := p.VerifyAttestation(t.Context(), res, digest, policy)
	require.NoError(t, err)
	require.True(t, verified)
	require.Equal(t, []string{
		"registry-1.docker.io",
		"registry-1.docker.io",
		"registry-1.docker.io",
	}, hosts)

	policy.Identity = "https://github.com/example/*"
	verified, err = p.VerifyAttestation(t.Context(), res, digest, policy)
	require.NoError(t, err)
	require.False(t, verified)
}

func TestVerifyAttestationInvalidResource(t *testing.T) {
	t.Parallel()

	p := docker.New(anon())
	verified, err := p.VerifyAttestation(
		t.Context(),
		"bad",
		"sha256:00",
		provider.AttestationPolicy{},
	)
	require.EqualError(t, err, "docker: invalid resource string")
	require.False(t, verified)
}
