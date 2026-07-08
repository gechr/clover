package golang_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/golang"
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

	p := golang.New()
	require.Equal(t, "go", p.Name())
	require.Empty(t, p.Keys())
}

func TestResource(t *testing.T) {
	t.Parallel()

	p := golang.New()

	// go.dev takes no provider-specific keys, so every directive resolves to the
	// same descriptor with the same label.
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)
	require.Equal(t, "go.dev", p.Describe(res))

	res, err = p.Resource(directiveOf(directive.KV{Key: "constraint", Value: "minor"}))
	require.NoError(t, err)
	require.Equal(t, "go.dev", p.Describe(res))
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "go", golang.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := golang.New()
	res, err := p.Resource(directiveOf())
	require.NoError(t, err)

	require.Equal(t,
		"https://go.dev/dl/#go1.26.5",
		p.URL(res, model.Candidate{Version: "go1.26.5"}),
	)
	// The current-version candidate carries the bare on-line value in Version and
	// the go-prefixed upstream form in Ref; the anchor must use the ref's prefix.
	require.Equal(t,
		"https://go.dev/dl/#go1.26.5",
		p.URL(res, model.Candidate{Version: "1.26.5", Ref: "go1.26.5"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "go1.26.5"}))
}

// TestNotRecencyOrderer locks the leaner design: the index returns the whole
// history in one response, so nothing is ever truncated and the provider does not
// claim the recency-ordered capability that only routes a truncation signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(golang.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
