package hashicorp_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/provider/hashicorp"
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

	p := hashicorp.New()
	require.Equal(t, "hashicorp", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 3)
	require.Equal(t, "product", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "enterprise", keys[1].Name)
	require.False(t, keys[1].Required)
	require.Equal(t, "build", keys[2].Name)
	require.False(t, keys[2].Required)
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
			name:         "product only",
			pairs:        []directive.KV{{Key: "product", Value: "terraform"}},
			wantDescribe: "releases.hashicorp.com/terraform",
		},
		{
			name: "enterprise true",
			pairs: []directive.KV{
				{Key: "product", Value: "vault"},
				{Key: "enterprise", Value: "true"},
			},
			wantDescribe: "releases.hashicorp.com/vault (enterprise)",
		},
		{
			name: "enterprise false",
			pairs: []directive.KV{
				{Key: "product", Value: "vault"},
				{Key: "enterprise", Value: "false"},
			},
			wantDescribe: "releases.hashicorp.com/vault",
		},
		{
			name: "build flavor",
			pairs: []directive.KV{
				{Key: "product", Value: "vault"},
				{Key: "build", Value: "ent.hsm.fips1403"},
			},
			wantDescribe: "releases.hashicorp.com/vault (ent.hsm.fips1403)",
		},
		{
			name: "build takes precedence over enterprise in the label",
			pairs: []directive.KV{
				{Key: "product", Value: "nomad"},
				{Key: "enterprise", Value: "true"},
				{Key: "build", Value: "ent.musl"},
			},
			wantDescribe: "releases.hashicorp.com/nomad (ent.musl)",
		},
		{
			name:    "missing product",
			pairs:   nil,
			wantErr: true,
		},
		{
			name: "bad enterprise bool",
			pairs: []directive.KV{
				{Key: "product", Value: "vault"},
				{Key: "enterprise", Value: "yes"},
			},
			wantErr: true,
		},
	}

	p := hashicorp.New()
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

	require.Equal(t, "hashicorp", hashicorp.New().Describe("not-a-resource"))
}
