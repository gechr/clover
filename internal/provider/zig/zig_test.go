package zig_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/zig"
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

	p := zig.New()
	require.Equal(t, "zig", p.Name())
	require.Empty(t, p.Keys())
}

func TestResource(t *testing.T) {
	t.Parallel()

	p := zig.New()

	// ziglang.org takes no provider-specific keys, so every directive resolves to
	// the same descriptor with the same label.
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	require.Equal(t, "ziglang.org", p.Describe(res))

	res, err = p.Resource(directiveOf(directive.KV{Key: "constraint", Value: "minor"}))
	require.NoError(t, err)
	require.Equal(t, "ziglang.org", p.Describe(res))
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "zig", zig.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := zig.New()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)

	require.Equal(t,
		"https://ziglang.org/download/0.16.0/",
		p.URL(res, model.Candidate{Version: "0.16.0"}),
	)
	// The version is clean semver and is also the upstream ref, so a candidate
	// carrying both links the same way.
	require.Equal(t,
		"https://ziglang.org/download/0.16.0/",
		p.URL(res, model.Candidate{Version: "0.16.0", Ref: "0.16.0"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "0.16.0"}))
}

// TestNotRecencyOrderer locks the leaner design: the index returns the whole
// history in one response, so nothing is ever truncated and the provider does not
// claim the recency-ordered capability that only routes a truncation signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(zig.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
