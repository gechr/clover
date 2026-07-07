package terraform_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/terraform"
	"github.com/stretchr/testify/require"
)

func directiveOf(pairs ...directive.KV) directive.Directive {
	return directive.Directive{Pairs: pairs}
}

// roundTripFunc adapts a function to an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// sourceAWS is the directive pair every resource test starts from.
var sourceAWS = directive.KV{Key: "source", Value: "hashicorp/aws"}

func TestNameAndKeys(t *testing.T) {
	t.Parallel()

	tf := terraform.New(terraform.Terraform)
	require.Equal(t, "terraform", tf.Name())

	tofu := terraform.New(terraform.OpenTofu)
	require.Equal(t, "opentofu", tofu.Name())

	keys := tf.Keys()
	require.Len(t, keys, 2)
	require.Equal(t, "source", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "host", keys[1].Name)
	require.False(t, keys[1].Required)
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
			name:         "source on the default host",
			pairs:        []directive.KV{sourceAWS},
			wantDescribe: "registry.terraform.io/hashicorp/aws",
		},
		{
			name: "host override",
			pairs: []directive.KV{
				sourceAWS,
				{Key: "host", Value: "registry.example.com"},
			},
			wantDescribe: "registry.example.com/hashicorp/aws",
		},
		{
			name:    "missing source",
			pairs:   nil,
			wantErr: `terraform: "source" must be namespace/name, got ""`,
		},
		{
			name:    "bare source",
			pairs:   []directive.KV{{Key: "source", Value: "aws"}},
			wantErr: `terraform: "source" must be namespace/name, got "aws"`,
		},
		{
			name:    "source with too many segments",
			pairs:   []directive.KV{{Key: "source", Value: "registry.terraform.io/hashicorp/aws"}},
			wantErr: `terraform: "source" must be namespace/name, got "registry.terraform.io/hashicorp/aws"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := terraform.New(terraform.Terraform)
			r, err := p.Resource(directiveOf(tt.pairs...))
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantDescribe, p.Describe(r))
		})
	}
}

func TestOpenTofuDefaultsToItsRegistry(t *testing.T) {
	t.Parallel()

	p := terraform.New(terraform.OpenTofu)
	r, err := p.Resource(directiveOf(sourceAWS))
	require.NoError(t, err)
	require.Equal(t, "registry.opentofu.org/hashicorp/aws", p.Describe(r))
}

func TestDescribeInvalidResource(t *testing.T) {
	t.Parallel()

	p := terraform.New(terraform.Terraform)
	require.Equal(t, "terraform", p.Describe("not a resource"))
}

func TestDiscoverInvalidResource(t *testing.T) {
	t.Parallel()

	p := terraform.New(terraform.Terraform)
	_, err := p.Discover(t.Context(), "not a resource")
	require.EqualError(t, err, "terraform: invalid resource string")
}

func TestURL(t *testing.T) {
	t.Parallel()

	tf := terraform.New(terraform.Terraform)
	r, err := tf.Resource(directiveOf(sourceAWS))
	require.NoError(t, err)

	linker, ok := provider.Provider(tf).(provider.Linker)
	require.True(t, ok, "the registry provider must link resolved versions")
	require.Equal(t,
		"https://registry.terraform.io/providers/hashicorp/aws/6.39.0",
		linker.URL(r, model.Candidate{Version: "6.39.0"}))

	tofu := terraform.New(terraform.OpenTofu)
	rt, err := tofu.Resource(directiveOf(sourceAWS))
	require.NoError(t, err)
	require.Equal(t,
		"https://search.opentofu.org/provider/hashicorp/aws/v6.39.0",
		tofu.URL(rt, model.Candidate{Version: "6.39.0"}))
}

// TestURLPrivateHost confirms a private registry yields no web link, since only
// the public registries have a known UI.
func TestURLPrivateHost(t *testing.T) {
	t.Parallel()

	p := terraform.New(terraform.Terraform)
	r, err := p.Resource(directiveOf(
		sourceAWS,
		directive.KV{Key: "host", Value: "registry.example.com"},
	))
	require.NoError(t, err)
	require.Empty(t, p.URL(r, model.Candidate{Version: "6.39.0"}))
	require.Empty(t, p.URL(r, model.Candidate{}), "no version, no link")
	require.Empty(t, p.URL("not a resource", model.Candidate{Version: "6.39.0"}))
}

// TestNotRecencyOrderer locks in that the registry provider does not claim
// recency ordering: the versions endpoint returns the whole unordered history
// in one response, so there is never an unread page for --deep to reach.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	var p provider.Provider = terraform.New(terraform.Terraform)
	_, ok := p.(provider.RecencyOrderer)
	require.False(t, ok, "one full fetch has no pagination to order")
}
