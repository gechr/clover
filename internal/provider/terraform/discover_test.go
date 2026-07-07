package terraform_test

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/directive"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider/terraform"
	"github.com/stretchr/testify/require"
)

// fixtureProvider serves the real, verbatim testdata captures: the service
// discovery document for the well-known path and a trimmed-but-unmodified
// slice of hashicorp/aws's versions response (spanning the oldest release,
// prereleases, and non-sorted ordering) for everything else. It fails the test
// on any unexpected request path.
func fixtureProvider(t *testing.T, reg terraform.Registry) *terraform.Provider {
	t.Helper()
	wellknown, err := os.ReadFile(filepath.Join("testdata", "wellknown.json"))
	require.NoError(t, err)
	versions, err := os.ReadFile(filepath.Join("testdata", "versions.json"))
	require.NoError(t, err)

	return terraform.New(reg, terraform.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var body []byte
			switch req.URL.Path {
			case "/.well-known/terraform.json":
				body = wellknown
			case "/v1/providers/hashicorp/aws/versions":
				body = versions
			default:
				t.Errorf("unexpected request path %q", req.URL.Path)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Request:    req,
			}, nil
		}),
	))
}

func versions(candidates []model.Candidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Version
	}
	return out
}

// TestFixtureDiscover covers the full path: service discovery resolves the
// providers.v1 base, the versions endpoint is fetched under it, and every
// version surfaces verbatim in the response's own (unsorted) order with a
// parsed semver and no publication date - the endpoint carries none, so
// cooldown is inert.
func TestFixtureDiscover(t *testing.T) {
	t.Parallel()

	p := fixtureProvider(t, terraform.Terraform)
	r, err := p.Resource(directiveOf(sourceAWS))
	require.NoError(t, err)

	got, err := p.Discover(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{
		"2.50.0", "6.0.0-beta1", "6.0.0-beta3", "6.16.0", "5.65.0", "2.33.0", "5.90.0",
	}, versions(got))
	for _, c := range got {
		require.NotNil(t, c.Semver, "registry versions are semver: %s", c.Version)
		require.True(t, c.PublishedAt.IsZero(), "the endpoint carries no dates")
		require.Empty(t, c.Commit)
		require.Empty(t, c.Digest)
	}
}

// TestDiscoverRequestsGoToTheResourceHost confirms both fetches target the
// host the resource carries, so a host override reaches the private registry.
func TestDiscoverRequestsGoToTheResourceHost(t *testing.T) {
	t.Parallel()

	var hosts []string
	p := terraform.New(terraform.Terraform, terraform.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			hosts = append(hosts, req.URL.Host)
			body := `{"providers.v1":"/v1/providers/"}`
			if strings.HasSuffix(req.URL.Path, "/versions") {
				body = `{"versions":[{"version":"1.0.0"}]}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	))
	r, err := p.Resource(directiveOf(
		sourceAWS,
		directive.KV{Key: "host", Value: "registry.example.com"},
	))
	require.NoError(t, err)

	got, err := p.Discover(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, versions(got))
	require.Equal(t, []string{"registry.example.com", "registry.example.com"}, hosts)
}

// TestDiscoverAbsoluteServiceBase covers a discovery document pointing the
// providers.v1 service at another origin, which the protocol allows.
func TestDiscoverAbsoluteServiceBase(t *testing.T) {
	t.Parallel()

	var paths []string
	p := terraform.New(terraform.Terraform, terraform.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			paths = append(paths, req.URL.Host+req.URL.Path)
			body := `{"providers.v1":"https://api.example.com/registry/"}`
			if req.URL.Host == "api.example.com" {
				body = `{"versions":[{"version":"1.2.3"}]}`
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	))
	r, err := p.Resource(directiveOf(sourceAWS))
	require.NoError(t, err)

	got, err := p.Discover(t.Context(), r)
	require.NoError(t, err)
	require.Equal(t, []string{"1.2.3"}, versions(got))
	require.Equal(t, []string{
		"registry.terraform.io/.well-known/terraform.json",
		"api.example.com/registry/hashicorp/aws/versions",
	}, paths)
}

// TestDiscoverMissingService covers a host whose discovery document does not
// offer the providers.v1 service.
func TestDiscoverMissingService(t *testing.T) {
	t.Parallel()

	p := terraform.New(terraform.Terraform, terraform.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"modules.v1":"/v1/modules/"}`)),
				Request:    req,
			}, nil
		}),
	))
	r, err := p.Resource(directiveOf(sourceAWS))
	require.NoError(t, err)

	_, err = p.Discover(t.Context(), r)
	require.EqualError(
		t,
		err,
		`terraform: host "registry.terraform.io" does not offer the providers.v1 service`,
	)
}

// TestDiscoverHTTPError covers a non-200 versions response, surfacing the
// upstream's own message.
func TestDiscoverHTTPError(t *testing.T) {
	t.Parallel()

	p := terraform.New(terraform.OpenTofu, terraform.WithTransport(
		roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/.well-known/terraform.json" {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{},
					Body: io.NopCloser(
						strings.NewReader(`{"providers.v1":"/v1/providers/"}`),
					),
					Request: req,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader(`{"errors":["Not Found"]}`)),
				Request:    req,
			}, nil
		}),
	))
	r, err := p.Resource(directiveOf(directive.KV{Key: "source", Value: "acme/missing"}))
	require.NoError(t, err)

	_, err = p.Discover(t.Context(), r)
	require.EqualError(
		t,
		err,
		`opentofu: GET https://registry.opentofu.org/v1/providers/acme/missing/versions: {"errors":["Not Found"]} (404 Not Found)`,
	)
}
