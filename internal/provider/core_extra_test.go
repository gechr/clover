package provider_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/oci"
	"github.com/gechr/clover/internal/provider"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/require"
)

// ociRoundTrip adapts a function to an http.RoundTripper.
type ociRoundTrip func(*http.Request) (*http.Response, error)

func (f ociRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// ociAnonKeychain always resolves to anonymous access, so tests never read the
// developer's real docker login.
type ociAnonKeychain struct{}

func (ociAnonKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

func ociJSON(req *http.Request, body string) *http.Response {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func ociClient(rt http.RoundTripper) *oci.Client {
	return oci.New(oci.WithTransport(rt), oci.WithKeychain(ociAnonKeychain{}))
}

func TestDiscoverOCITags(t *testing.T) {
	t.Parallel()

	transport := ociRoundTrip(func(req *http.Request) (*http.Response, error) {
		return ociJSON(req, `{"tags":["1.0.0","1.2.0","latest"]}`), nil
	})
	repo := oci.Repo{Host: "registry.example.com", Repository: "team/img"}

	candidates, err := provider.DiscoverOCITags(t.Context(), ociClient(transport), repo,
		"registry.example.com/team/img", "https://registry.example.com/team/img")
	require.NoError(t, err)

	require.Len(t, candidates, 3)
	require.Equal(t, "1.0.0", candidates[0].Version)
	require.NotNil(t, candidates[0].Semver)
	require.Equal(t, "latest", candidates[2].Version)
	require.Nil(t, candidates[2].Semver, "a non-semver tag yields a nil Semver")
}

func TestDiscoverOCITagsEmpty(t *testing.T) {
	t.Parallel()

	transport := ociRoundTrip(func(req *http.Request) (*http.Response, error) {
		return ociJSON(req, `{"tags":[]}`), nil
	})
	repo := oci.Repo{Host: "registry.example.com", Repository: "team/img"}

	candidates, err := provider.DiscoverOCITags(t.Context(), ociClient(transport), repo, "d", "u")
	require.NoError(t, err)
	require.Empty(t, candidates)
}

func TestDiscoverOCITagsShallowNotesTruncation(t *testing.T) {
	t.Parallel()

	transport := ociRoundTrip(func(req *http.Request) (*http.Response, error) {
		resp := ociJSON(req, `{"tags":["1.0.0","1.1.0"]}`)
		resp.Header.Set("Link", `</v2/team/img/tags/list?n=100&last=1.1.0>; rel="next"`)
		return resp, nil
	})
	repo := oci.Repo{Host: "registry.example.com", Repository: "team/img"}

	var truncated []provider.Truncation
	ctx := provider.WithTruncationSink(t.Context(),
		func(tr provider.Truncation) { truncated = append(truncated, tr) })

	candidates, err := provider.DiscoverOCITags(ctx, ociClient(transport), repo,
		"registry.example.com/team/img", "https://registry.example.com/team/img")
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, []provider.Truncation{{
		Resource: "registry.example.com/team/img",
		URL:      "https://registry.example.com/team/img",
	}}, truncated, "a shallow lookup with more pages notes the truncation")
}

func TestDiscoverOCITagsDeepExhausts(t *testing.T) {
	t.Parallel()

	transport := ociRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("last") == "" {
			resp := ociJSON(req, `{"tags":["1.0.0","1.1.0"]}`)
			resp.Header.Set("Link", `</v2/team/img/tags/list?n=100&last=1.1.0>; rel="next"`)
			return resp, nil
		}
		return ociJSON(req, `{"tags":["1.2.0"]}`), nil
	})
	repo := oci.Repo{Host: "registry.example.com", Repository: "team/img"}

	var truncated []provider.Truncation
	ctx := provider.WithTruncationSink(provider.WithDeep(t.Context(), true),
		func(tr provider.Truncation) { truncated = append(truncated, tr) })

	candidates, err := provider.DiscoverOCITags(ctx, ociClient(transport), repo, "d", "u")
	require.NoError(t, err)
	require.Len(t, candidates, 3)
	require.Empty(t, truncated, "a deep lookup exhausts the pages, so nothing is truncated")
}

func TestDiscoverOCITagsServerError(t *testing.T) {
	t.Parallel()

	transport := ociRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})
	repo := oci.Repo{Host: "registry.example.com", Repository: "team/img"}

	candidates, err := provider.DiscoverOCITags(t.Context(), ociClient(transport), repo, "d", "u")
	require.Error(t, err)
	require.Nil(t, candidates)
}

// keyedProvider is a Provider whose Keys drive KeyNames.
type keyedProvider struct {
	keys []provider.Key
}

func (keyedProvider) Name() string                      { return "keyed" }
func (p keyedProvider) Keys() []provider.Key            { return p.keys }
func (keyedProvider) Describe(provider.Resource) string { return "keyed" }

func (keyedProvider) Resource(directive.Directive) (provider.Resource, error) {
	return struct{}{}, nil
}

func (keyedProvider) Discover(context.Context, provider.Resource) ([]model.Candidate, error) {
	return nil, nil
}

func TestKeyNames(t *testing.T) {
	t.Parallel()

	p := keyedProvider{keys: []provider.Key{{Name: "a"}, {Name: "b", Required: true}}}
	require.Equal(t, []string{"a", "b"}, provider.KeyNames(p))

	require.Empty(t, provider.KeyNames(keyedProvider{}))
}

func TestRegisterAll(t *testing.T) {
	provider.RegisterAll(stubProvider{name: "ra-one"}, stubProvider{name: "ra-two"})

	one, ok := provider.Get("ra-one")
	require.True(t, ok)
	require.Equal(t, "ra-one", one.Name())

	two, ok := provider.Get("ra-two")
	require.True(t, ok)
	require.Equal(t, "ra-two", two.Name())

	provider.RegisterAll() // zero args is a no-op
}

func TestStatusError(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		Status: "429 Too Many Requests",
		Body:   io.NopCloser(strings.NewReader("  rate limited  ")),
	}
	require.EqualError(t, provider.StatusError("list tags", resp),
		"list tags: rate limited (429 Too Many Requests)")

	empty := &http.Response{
		Status: "500 Internal Server Error",
		Body:   io.NopCloser(strings.NewReader("")),
	}
	require.EqualError(t, provider.StatusError("list tags", empty),
		"list tags:  (500 Internal Server Error)")
}
