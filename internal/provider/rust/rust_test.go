package rust_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/rust"
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

	p := rust.New()
	require.Equal(t, "rust", p.Name())
	require.Equal(t, []provider.Key{{Name: "channel"}}, p.Keys())
}

func TestResource(t *testing.T) {
	t.Parallel()

	p := rust.New()

	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	require.Equal(t, "rust-lang.org", p.Describe(res))

	res, err = p.Resource(directiveOf(directive.KV{Key: "channel", Value: "stable"}))
	require.NoError(t, err)
	require.Equal(t, "rust-lang.org", p.Describe(res))

	res, err = p.Resource(directiveOf(directive.KV{Key: "channel", Value: "beta"}))
	require.NoError(t, err)
	require.Equal(t, "rust-lang.org (beta)", p.Describe(res))
}

func TestResourceNightlyChannel(t *testing.T) {
	t.Parallel()

	_, err := rust.New().Resource(directiveOf(directive.KV{Key: "channel", Value: "nightly"}))
	require.EqualError(
		t,
		err,
		`rust: channel "nightly" is not trackable: nightly builds are dated snapshots, not versions`,
	)
}

func TestResourceInvalidChannel(t *testing.T) {
	t.Parallel()

	_, err := rust.New().Resource(directiveOf(directive.KV{Key: "channel", Value: "miri"}))
	require.EqualError(t, err, `rust: invalid channel "miri" (expected "stable" or "beta")`)
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "rust", rust.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := rust.New()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)

	// A stable release links to its release-notes tag page.
	require.Equal(
		t,
		"https://github.com/rust-lang/rust/releases/tag/1.97.0",
		p.URL(res, model.Candidate{Version: "1.97.0", Ref: "1.97.0"}),
	)
	// The synthesized current-version candidate carries no Ref; the bare
	// version links the same page.
	require.Equal(
		t,
		"https://github.com/rust-lang/rust/releases/tag/1.97.0",
		p.URL(res, model.Candidate{Version: "1.97.0"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "1.97.0"}))

	// A beta snapshot has no tag on rust-lang/rust, so it is not linked.
	beta, err := p.Resource(directiveOf(directive.KV{Key: "channel", Value: "beta"}))
	require.NoError(t, err)
	require.Empty(t, p.URL(beta, model.Candidate{Version: "1.98.0-beta.1", Ref: "1.98.0-beta.1"}))
}

// TestNotRecencyOrderer locks the leaner design: the index returns the whole
// history in one response, so nothing is ever truncated and the provider does
// not claim the recency-ordered capability that only routes a truncation signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(rust.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
