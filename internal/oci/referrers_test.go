package oci_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/oci"
	"github.com/stretchr/testify/require"
)

const (
	bundleType = "application/vnd.dev.sigstore.bundle.v0.3+json"
	subject    = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestReferrerArtifacts(t *testing.T) {
	t.Parallel()

	var paths []string
	client := newClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		switch req.URL.Path {
		case "/v2/team/app/referrers/" + subject:
			require.Equal(t, "application/vnd.oci.image.index.v1+json", req.Header.Get("Accept"))
			return jsonResponse(req, `{"manifests":[`+
				`{"artifactType":"application/example","digest":"sha256:ignored"},`+
				`{"artifactType":"`+bundleType+`","digest":"sha256:manifest"}`+
				`]}`), nil
		case "/v2/team/app/manifests/sha256:manifest":
			return jsonResponse(req, `{"layers":[`+
				`{"mediaType":"application/example","digest":"sha256:ignored-layer"},`+
				`{"mediaType":"`+bundleType+`","digest":"sha256:bundle"}`+
				`]}`), nil
		case "/v2/team/app/blobs/sha256:bundle":
			return jsonResponse(req, `{"bundle":true}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %q", req.URL.Path)
		}
	}))

	got, err := client.ReferrerArtifacts(t.Context(), oci.Repo{
		Host: "registry.example.com", Repository: "team/app",
	}, subject, "application/vnd.dev.sigstore.bundle")
	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte(`{"bundle":true}`)}, got)
	require.Equal(t, []string{
		"/v2/team/app/referrers/" + subject,
		"/v2/team/app/manifests/sha256:manifest",
		"/v2/team/app/blobs/sha256:bundle",
	}, paths)
}

func TestReferrerArtifactsFallback(t *testing.T) {
	t.Parallel()

	hex := strings.TrimPrefix(subject, "sha256:")
	client := newClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v2/team/app/referrers/" + subject:
			return statusResponse(req, http.StatusMethodNotAllowed), nil
		case "/v2/team/app/manifests/sha256-" + hex:
			return jsonResponse(req, `{"manifests":[]}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %q", req.URL.Path)
		}
	}))

	got, err := client.ReferrerArtifacts(t.Context(), oci.Repo{
		Host: "registry.example.com", Repository: "team/app",
	}, subject, "application/vnd.dev.sigstore.bundle")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestReferrerArtifactsFallbackMissing(t *testing.T) {
	t.Parallel()

	client := newClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return statusResponse(req, http.StatusNotFound), nil
	}))

	got, err := client.ReferrerArtifacts(t.Context(), oci.Repo{
		Host: "registry.example.com", Repository: "team/app",
	}, subject, "application/vnd.dev.sigstore.bundle")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestReferrerArtifactsChallengeRetry(t *testing.T) {
	t.Parallel()

	challenged := map[string]bool{}
	client := newClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "auth.example.com" {
			return jsonResponse(req, `{"token":"registry-token"}`), nil
		}
		if !challenged[req.URL.Path] {
			challenged[req.URL.Path] = true
			return challengeResponse(req), nil
		}
		require.Equal(t, "Bearer registry-token", req.Header.Get("Authorization"))
		switch {
		case strings.Contains(req.URL.Path, "/referrers/"):
			return jsonResponse(
				req,
				`{"manifests":[{"artifactType":"`+bundleType+`","digest":"sha256:manifest"}]}`,
			), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			return jsonResponse(
				req,
				`{"layers":[{"mediaType":"`+bundleType+`","digest":"sha256:bundle"}]}`,
			), nil
		case strings.Contains(req.URL.Path, "/blobs/"):
			return jsonResponse(req, `{}`), nil
		default:
			return nil, fmt.Errorf("unexpected request path %q", req.URL.Path)
		}
	}))

	got, err := client.ReferrerArtifacts(t.Context(), oci.Repo{
		Host: "registry.example.com", Repository: "team/app",
	}, subject, "application/vnd.dev.sigstore.bundle")
	require.NoError(t, err)
	require.Equal(t, [][]byte{[]byte(`{}`)}, got)
	require.Equal(t, map[string]bool{
		"/v2/team/app/referrers/" + subject:      true,
		"/v2/team/app/manifests/sha256:manifest": true,
		"/v2/team/app/blobs/sha256:bundle":       true,
	}, challenged)
}

func TestReferrerArtifactsBlobSizeLimit(t *testing.T) {
	t.Parallel()

	client := newClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/referrers/"):
			return jsonResponse(
				req,
				`{"manifests":[{"artifactType":"`+bundleType+`","digest":"sha256:manifest"}]}`,
			), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			return jsonResponse(
				req,
				`{"layers":[{"mediaType":"`+bundleType+`","digest":"sha256:bundle"}]}`,
			), nil
		case strings.Contains(req.URL.Path, "/blobs/"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(io.LimitReader(zeroReader{}, (4<<20)+1)),
				Request:    req,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected request path %q", req.URL.Path)
		}
	}))

	_, err := client.ReferrerArtifacts(t.Context(), oci.Repo{
		Host: "registry.example.com", Repository: "team/app",
	}, subject, "application/vnd.dev.sigstore.bundle")
	require.EqualError(t, err, "oci: blob sha256:bundle exceeds 4194304 bytes")
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func statusResponse(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       http.NoBody,
		Request:    req,
	}
}
