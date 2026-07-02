package node_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/node"
	"github.com/stretchr/testify/require"
)

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

// resourceFor builds a validated resource for the given directive pairs.
func resourceFor(t *testing.T, p *node.Provider, pairs ...directive.KV) provider.Resource {
	t.Helper()
	res, err := p.Resource(directiveOf(pairs...))
	require.NoError(t, err)
	return res
}

// newProvider returns a provider whose transport serves the given index body.
func newProvider(body string) *node.Provider {
	return node.New(node.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(req, body), nil
		}),
	))
}

// versions extracts the version strings from candidates, in order.
func versions(candidates []model.Candidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Version
	}
	return out
}

// TestDiscoverSkipsBlankVersions covers an index entry with no version: it is
// dropped rather than surfaced as an empty candidate.
func TestDiscoverSkipsBlankVersions(t *testing.T) {
	t.Parallel()

	const body = `[
		{"version": "", "date": "2024-06-11", "lts": false},
		{"version": "v22.3.0", "date": "2024-06-11", "lts": false}
	]`

	p := newProvider(body)
	candidates, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t, []string{"v22.3.0"}, versions(candidates))
}

func TestDiscoverDateOnlyUsesEndOfUTCDate(t *testing.T) {
	t.Parallel()

	const body = `[
		{"version": "v22.3.0", "date": "2026-01-02", "lts": false}
	]`

	p := newProvider(body)
	candidates, err := p.Discover(t.Context(), resourceFor(t, p))
	require.NoError(t, err)
	require.Equal(t,
		time.Date(2026, 1, 2, 23, 59, 59, 0, time.UTC),
		candidates[0].PublishedAt,
	)
}

func TestDiscoverError(t *testing.T) {
	t.Parallel()

	p := node.New(node.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Body:       io.NopCloser(strings.NewReader("not found")),
				Request:    req,
			}, nil
		}),
	))

	_, err := p.Discover(t.Context(), resourceFor(t, p))
	require.Error(t, err)
}
