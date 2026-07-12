package npm_test

import (
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/npm"
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

	p := npm.New()
	require.Equal(t, "npm", p.Name())

	keys := p.Keys()
	require.Len(t, keys, 4)
	require.Equal(t, "package", keys[0].Name)
	require.True(t, keys[0].Required)
	require.Equal(t, "dist-tag", keys[1].Name)
	require.False(t, keys[1].Required)
	require.Equal(t, "deprecated", keys[2].Name)
	require.False(t, keys[2].Required)
	require.Equal(t, "registry", keys[3].Name)
	require.False(t, keys[3].Required)
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
			name:         "package names the package",
			pairs:        []directive.KV{{Key: "package", Value: "left-pad"}},
			wantDescribe: "npmjs.com/left-pad",
		},
		{
			name:         "scoped package keeps its scope",
			pairs:        []directive.KV{{Key: "package", Value: "@vue/reactivity"}},
			wantDescribe: "npmjs.com/@vue/reactivity",
		},
		{
			name: "custom registry",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "registry", Value: "https://npm.internal.corp"},
			},
			wantDescribe: "npm.internal.corp/left-pad",
		},
		{
			name: "custom registry tolerates a trailing slash",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "registry", Value: "http://npm.internal.corp/"},
			},
			wantDescribe: "npm.internal.corp/left-pad",
		},
		{
			name: "dist-tag names a channel",
			pairs: []directive.KV{
				{Key: "package", Value: "@vue/reactivity"},
				{Key: "dist-tag", Value: "beta"},
			},
			wantDescribe: "npmjs.com/@vue/reactivity@beta",
		},
		{
			name:    "missing package",
			pairs:   nil,
			wantErr: `npm: "package" is required`,
		},
		{
			name:         "legacy uppercase package",
			pairs:        []directive.KV{{Key: "package", Value: "JSONStream"}},
			wantDescribe: "npmjs.com/JSONStream",
		},
		{
			name:    "package with whitespace",
			pairs:   []directive.KV{{Key: "package", Value: "left pad"}},
			wantErr: `npm: "package" must be a valid package name, got "left pad"`,
		},
		{
			name:    "package with a leading dot",
			pairs:   []directive.KV{{Key: "package", Value: ".left-pad"}},
			wantErr: `npm: "package" must be a valid package name, got ".left-pad"`,
		},
		{
			name:    "scope without a name",
			pairs:   []directive.KV{{Key: "package", Value: "@vue"}},
			wantErr: `npm: "package" must be a valid package name, got "@vue"`,
		},
		{
			name:    "unscoped package with a slash",
			pairs:   []directive.KV{{Key: "package", Value: "vue/reactivity"}},
			wantErr: `npm: "package" must be a valid package name, got "vue/reactivity"`,
		},
		{
			name: "invalid deprecated",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "deprecated", Value: "yes"},
			},
			wantErr: `npm: "deprecated" must be true or false, got "yes"`,
		},
		{
			name: "empty dist-tag",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "dist-tag", Value: ""},
			},
			wantErr: `npm: "dist-tag" must not be empty`,
		},
		{
			name: "dist-tag with whitespace",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "dist-tag", Value: "beta channel"},
			},
			wantErr: `npm: "dist-tag" must not contain whitespace, got "beta channel"`,
		},
		{
			name:    "empty package",
			pairs:   []directive.KV{{Key: "package", Value: ""}},
			wantErr: `npm: "package" is required`,
		},
		{
			name: "registry without a scheme",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "registry", Value: "npm.internal.corp"},
			},
			wantErr: `npm: "registry" must start with https:// or http://, got "npm.internal.corp"`,
		},
		{
			name: "registry with an unsupported scheme",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "registry", Value: "oci://npm.internal.corp"},
			},
			wantErr: `npm: "registry" must start with https:// or http://, got "oci://npm.internal.corp"`,
		},
		{
			name: "registry without a host",
			pairs: []directive.KV{
				{Key: "package", Value: "left-pad"},
				{Key: "registry", Value: "https://"},
			},
			wantErr: `npm: registry "https://" has no registry host`,
		},
	}

	p := npm.New()
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

	require.Equal(t, "npm", npm.New().Describe("not-a-resource"))
}

func TestURL(t *testing.T) {
	t.Parallel()

	p := npm.New()
	res := resourceFor(t, p, directive.KV{Key: "package", Value: "left-pad"})

	require.Equal(t,
		"https://www.npmjs.com/package/left-pad/v/1.3.0",
		p.URL(res, model.Candidate{Version: "1.3.0"}),
	)
	// The current-version candidate carries the on-line value in Version and the
	// reconstructed upstream form in Ref; npm publishes bare semver, so the two
	// agree and either path links the same page.
	require.Equal(t,
		"https://www.npmjs.com/package/left-pad/v/1.3.0",
		p.URL(res, model.Candidate{Version: "1.3.0", Ref: "1.3.0"}),
	)

	// A scoped name keeps its literal slash on the web path.
	scoped := resourceFor(t, p, directive.KV{Key: "package", Value: "@vue/reactivity"})
	require.Equal(t,
		"https://www.npmjs.com/package/@vue/reactivity/v/3.5.39",
		p.URL(scoped, model.Candidate{Version: "3.5.39"}),
	)

	require.Empty(t, p.URL(res, model.Candidate{}))
	require.Empty(t, p.URL("not-a-resource", model.Candidate{Version: "1.3.0"}))

	// A custom registry's web UI (if any) is unknown, so no link is offered.
	custom := resourceFor(t, p,
		directive.KV{Key: "package", Value: "left-pad"},
		directive.KV{Key: "registry", Value: "https://npm.internal.corp"},
	)
	require.Empty(t, p.URL(custom, model.Candidate{Version: "1.3.0"}))
}

// TestNotRecencyOrderer locks the leaner design: the packument returns the whole
// version history in one response, so nothing is ever truncated and the provider
// does not claim the recency-ordered capability that only routes a truncation
// signal. The versions map is keyed in publish order anyway, which selection
// never relies on.
func TestNotRecencyOrderer(t *testing.T) {
	t.Parallel()

	_, ok := any(npm.New()).(provider.RecencyOrderer)
	require.False(t, ok)
}
