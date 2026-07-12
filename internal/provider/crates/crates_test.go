package crates_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/crates"
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

	p := crates.New()
	require.Equal(t, "crates", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 1)
	require.Equal(t, "package", keys[0].Name)
	require.True(t, keys[0].Required)
}

func TestResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		pairs        []directive.KV
		wantErr      string
		wantDescribe string
	}{
		{
			name:    "package is required",
			pairs:   nil,
			wantErr: `crates: "package" is required`,
		},
		{
			name:         "plain name",
			pairs:        []directive.KV{{Key: "package", Value: "serde"}},
			wantDescribe: "crates.io/serde",
		},
		{
			name:         "underscore is kept exact",
			pairs:        []directive.KV{{Key: "package", Value: "serde_json"}},
			wantDescribe: "crates.io/serde_json",
		},
		{
			name:         "mixed case is kept exact",
			pairs:        []directive.KV{{Key: "package", Value: "Inflector"}},
			wantDescribe: "crates.io/Inflector",
		},
		{
			name:    "leading separator is invalid",
			pairs:   []directive.KV{{Key: "package", Value: "-serde"}},
			wantErr: `crates: "package" is not a valid crate name, got "-serde"`,
		},
		{
			name:    "dotted name is invalid",
			pairs:   []directive.KV{{Key: "package", Value: "bad.name"}},
			wantErr: `crates: "package" is not a valid crate name, got "bad.name"`,
		},
		{
			name:    "whitespace is invalid",
			pairs:   []directive.KV{{Key: "package", Value: "bad name"}},
			wantErr: `crates: "package" is not a valid crate name, got "bad name"`,
		},
	}

	p := crates.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := p.Resource(directiveOf(tt.pairs...))
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDescribe, p.Describe(res))
		})
	}
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	require.Equal(t, "crates", crates.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := crates.New()
	res := resourceFor(t, p, directive.KV{Key: "package", Value: "clap"})

	// A discovered candidate links via its ref, the raw form the registry
	// publishes.
	require.Equal(t,
		"https://crates.io/crates/clap/4.0.0-rc.3",
		p.URL(res, model.Candidate{Version: "4.0.0-rc.3", Ref: "4.0.0-rc.3"}),
	)
	// The synthesized current-version candidate carries the bare on-line value.
	require.Equal(t,
		"https://crates.io/crates/clap/4.6.1",
		p.URL(res, model.Candidate{Version: "4.6.1"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "4.6.1"}))
}

// TestNotRecencyOrderer locks the leaner design: the registry API returns the
// whole history in one response, so nothing is ever truncated and the provider
// does not claim the recency-ordered capability that only routes a truncation
// signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(crates.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
