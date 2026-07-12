package python_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/python"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestNameAndKeys(t *testing.T) {
	t.Parallel()

	p := python.New()
	require.Equal(t, "python", p.Name())
	require.Empty(t, p.Keys())
}

func TestResource(t *testing.T) {
	t.Parallel()

	p := python.New()

	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	require.Equal(t, "python.org", p.Describe(res))

	res, err = p.Resource(directiveOf(directive.KV{Key: "constraint", Value: "minor"}))
	require.NoError(t, err)
	require.Equal(t, "python.org", p.Describe(res))
}

// TestResourceRejectsAsset covers the up-front guard: python.org publishes no
// release assets, so asset= is rejected at validation rather than failing every
// selection later.
func TestResourceRejectsAsset(t *testing.T) {
	t.Parallel()

	_, err := python.New().Resource(directiveOf(directive.KV{Key: "asset", Value: "*.tar.gz"}))
	require.EqualError(
		t,
		err,
		`python: "asset" is not supported, python.org publishes no release assets`,
	)
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "python", python.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := python.New()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)

	// The release page is reconstructed from the slug carried in Meta.
	require.Equal(
		t,
		"https://www.python.org/downloads/release/python-3146/",
		p.URL(
			res,
			model.Candidate{Version: "3.14.6", Meta: map[string]string{"slug": "python-3146"}},
		),
	)
	// A carried slug wins over derivation, covering the historic deviations
	// (3.3.5rc1's slug is python-335-rc1, not the derived python-335rc1).
	require.Equal(
		t,
		"https://www.python.org/downloads/release/python-335-rc1/",
		p.URL(
			res,
			model.Candidate{
				Version: "3.3.5-rc1",
				Meta:    map[string]string{"slug": "python-335-rc1"},
			},
		),
	)
	// No slug (the synthesized current-version candidate) derives one from the
	// version, preferring the raw Ref: dots and dashes drop.
	require.Equal(
		t,
		"https://www.python.org/downloads/release/python-3146/",
		p.URL(res, model.Candidate{Version: "3.14.6"}),
	)
	require.Equal(
		t,
		"https://www.python.org/downloads/release/python-3150b3/",
		p.URL(res, model.Candidate{Version: "3.15.0-b3", Ref: "3.15.0b3"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(
		t,
		p.URL("not-a-resource", model.Candidate{Meta: map[string]string{"slug": "python-3146"}}),
	)
}

// TestNotRecencyOrderer locks the leaner design: the API returns the whole
// history in one response, so nothing is ever truncated and the provider does not
// claim the recency-ordered capability that only routes a truncation signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(python.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
