package swift_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/swift"
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

	p := swift.New()
	require.Equal(t, "swift", p.Name())
	require.Empty(t, p.Keys())
}

func TestResource(t *testing.T) {
	t.Parallel()

	p := swift.New()

	// swift.org takes no provider-specific keys, so every directive resolves to
	// the same descriptor with the same label.
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	require.Equal(t, "swift.org", p.Describe(res))

	res, err = p.Resource(directiveOf(directive.KV{Key: "constraint", Value: "minor"}))
	require.NoError(t, err)
	require.Equal(t, "swift.org", p.Describe(res))
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "swift", swift.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := swift.New()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)

	const want = "https://github.com/swiftlang/swift/releases/tag/swift-6.3.3-RELEASE"

	// A discovered candidate carries the full tag in Ref.
	require.Equal(t, want,
		p.URL(res, model.Candidate{Version: "6.3.3", Ref: "swift-6.3.3-RELEASE"}))
	// The synthesized current-version candidate arrives bare (the pipeline
	// reconstructs only a v-style prefix), and any partial form still rebuilds
	// the full tag.
	require.Equal(t, want, p.URL(res, model.Candidate{Version: "6.3.3"}))
	require.Equal(t, want,
		p.URL(res, model.Candidate{Version: "6.3.3", Ref: "swift-6.3.3"}))
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "6.3.3"}))
}

// TestNotRecencyOrderer locks the leaner design: the index returns the whole
// history in one response, so nothing is ever truncated and the provider does not
// claim the recency-ordered capability that only routes a truncation signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(swift.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
