package checksum_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/checksum"
	"github.com/gechr/clover/internal/model"
	"github.com/stretchr/testify/require"
)

// failClient errors on any request, proving a source performed no HTTP.
func failClient(t *testing.T) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected HTTP request")
		return nil, errors.New("no request expected")
	})}
}

// serveBody answers every request with body.
func serveBody(body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
}

func TestResolveAutoUsesDigestNoFetch(t *testing.T) {
	t.Parallel()

	assets := []model.Asset{
		{Name: "tool_linux_amd64.tar.gz", Digest: "sha256:" + sumA, URL: "http://x/a"},
		{Name: "tool_windows.zip", Digest: "sha256:" + sumB, URL: "http://x/b"},
		{Name: "checksums.txt", URL: "http://x/checksums.txt"},
	}
	got, err := checksum.Resolve(t.Context(), failClient(t), checksum.Request{
		Source: "auto", Assets: assets, Pattern: "*linux_amd64*",
	})
	require.NoError(t, err)
	require.Equal(t, sumA, got, "auto pins the free digest without any HTTP")
}

func TestResolveChecksumsSibling(t *testing.T) {
	t.Parallel()

	assets := []model.Asset{
		{Name: "tool_linux_amd64.tar.gz", URL: "http://x/tool"}, // no digest
		{Name: "checksums.txt", URL: "http://x/checksums.txt"},
	}
	got, err := checksum.Resolve(t.Context(), serveBody(sumA+"  tool_linux_amd64.tar.gz\n"),
		checksum.Request{Source: "checksums", Assets: assets, Pattern: "*linux_amd64*"})
	require.NoError(t, err)
	require.Equal(t, sumA, got)
}

func TestResolveDownloadAndHash(t *testing.T) {
	t.Parallel()

	const content = "the-release-binary-bytes"
	sum := sha256.Sum256([]byte(content))

	assets := []model.Asset{{Name: "tool_linux_amd64.tar.gz", URL: "http://x/tool"}}
	got, err := checksum.Resolve(t.Context(), serveBody(content),
		checksum.Request{Source: "download", Assets: assets, Pattern: "*linux_amd64*"})
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(sum[:]), got)
}

func TestResolveVerify(t *testing.T) {
	t.Parallel()

	agree := []model.Asset{
		{Name: "tool_linux_amd64.tar.gz", Digest: "sha256:" + sumA, URL: "http://x/tool"},
		{Name: "checksums.txt", URL: "http://x/checksums.txt"},
	}
	client := serveBody(sumA + "  tool_linux_amd64.tar.gz\n")
	got, err := checksum.Resolve(t.Context(), client,
		checksum.Request{Source: "verify", Assets: agree, Pattern: "*linux_amd64*"})
	require.NoError(t, err)
	require.Equal(t, sumA, got)

	disagree := []model.Asset{
		{Name: "tool_linux_amd64.tar.gz", Digest: "sha256:" + sumB, URL: "http://x/tool"},
		{Name: "checksums.txt", URL: "http://x/checksums.txt"},
	}
	_, err = checksum.Resolve(t.Context(), client,
		checksum.Request{Source: "verify", Assets: disagree, Pattern: "*linux_amd64*"})
	require.ErrorContains(t, err, "disagree", "verify fails loud when the sources differ")
}

func TestResolveMatchErrors(t *testing.T) {
	t.Parallel()

	assets := []model.Asset{
		{Name: "a_linux.tar.gz", Digest: "sha256:" + sumA},
		{Name: "b_linux.tar.gz", Digest: "sha256:" + sumB},
	}
	_, err := checksum.Resolve(t.Context(), failClient(t),
		checksum.Request{Source: "digest", Assets: assets, Pattern: "*linux*"})
	require.ErrorContains(t, err, "matched 2 assets")

	_, err = checksum.Resolve(t.Context(), failClient(t),
		checksum.Request{Source: "digest", Assets: assets, Pattern: "*windows*"})
	require.ErrorContains(t, err, "no asset matched")
}
