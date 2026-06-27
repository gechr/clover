package node_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/node"
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

	p := node.New()
	require.Equal(t, "node", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 1)
	require.Equal(t, "lts", keys[0].Name)
	require.False(t, keys[0].Required)
}

func TestResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		pairs        []directive.KV
		wantErr      bool
		wantDescribe string
	}{
		{
			name:         "default tracks all releases",
			pairs:        nil,
			wantDescribe: "nodejs.org",
		},
		{
			name:         "lts true scopes to the LTS lines",
			pairs:        []directive.KV{{Key: "lts", Value: "true"}},
			wantDescribe: "nodejs.org (LTS)",
		},
		{
			name:         "lts false tracks all releases",
			pairs:        []directive.KV{{Key: "lts", Value: "false"}},
			wantDescribe: "nodejs.org",
		},
		{
			name:    "bad lts bool",
			pairs:   []directive.KV{{Key: "lts", Value: "yes"}},
			wantErr: true,
		},
	}

	p := node.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := p.Resource(directiveOf(tt.pairs...))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDescribe, p.Describe(res))
		})
	}
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "node", node.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := node.New()
	res := resourceFor(t, p)

	require.Equal(t,
		"https://nodejs.org/dist/v22.3.0/",
		p.URL(res, model.Candidate{Version: "v22.3.0"}),
	)
	// The current-version candidate carries the bare on-line value in Version and
	// the v-prefixed upstream form in Ref; the link must use the ref's prefix.
	require.Equal(t,
		"https://nodejs.org/dist/v24.18.0/",
		p.URL(res, model.Candidate{Version: "24.18.0", Ref: "v24.18.0"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "v22.3.0"}))
}

// TestNotRecencyOrderer locks the leaner design: the index returns the whole
// history in one response, so nothing is ever truncated and the provider does not
// claim the recency-ordered capability that only routes a truncation signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(node.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
