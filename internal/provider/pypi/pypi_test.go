package pypi_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/pypi"
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

	p := pypi.New()
	require.Equal(t, "pypi", p.Name())

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
			wantErr: `pypi: "package" is required`,
		},
		{
			name:         "plain name",
			pairs:        []directive.KV{{Key: "package", Value: "requests"}},
			wantDescribe: "pypi.org/requests",
		},
		{
			name:         "underscore normalizes to a dash",
			pairs:        []directive.KV{{Key: "package", Value: "uv_build"}},
			wantDescribe: "pypi.org/uv-build",
		},
		{
			name:         "dotted name normalizes to a dash",
			pairs:        []directive.KV{{Key: "package", Value: "ruamel.yaml"}},
			wantDescribe: "pypi.org/ruamel-yaml",
		},
		{
			name:         "mixed case lowers",
			pairs:        []directive.KV{{Key: "package", Value: "Django"}},
			wantDescribe: "pypi.org/django",
		},
		{
			name:    "leading separator is invalid",
			pairs:   []directive.KV{{Key: "package", Value: "-requests"}},
			wantErr: `pypi: "package" is not a valid package name, got "-requests"`,
		},
		{
			name:    "whitespace is invalid",
			pairs:   []directive.KV{{Key: "package", Value: "bad name"}},
			wantErr: `pypi: "package" is not a valid package name, got "bad name"`,
		},
	}

	p := pypi.New()
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

	require.Equal(t, "pypi", pypi.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := pypi.New()
	res := resourceFor(t, p, directive.KV{Key: "package", Value: "uv_build"})

	// A discovered candidate links via its raw PEP 440 ref, the form PyPI's
	// URLs publish.
	require.Equal(t,
		"https://pypi.org/project/uv-build/0.5.30rc1/",
		p.URL(res, model.Candidate{Version: "0.5.30-rc1", Ref: "0.5.30rc1"}),
	)
	// The synthesized current-version candidate carries the bare on-line value.
	require.Equal(t,
		"https://pypi.org/project/uv-build/0.11.16/",
		p.URL(res, model.Candidate{Version: "0.11.16"}),
	)
	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "0.11.16"}))
}

// TestNotRecencyOrderer locks the leaner design: the JSON API returns the whole
// history in one response, so nothing is ever truncated and the provider does
// not claim the recency-ordered capability that only routes a truncation
// signal.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(pypi.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
