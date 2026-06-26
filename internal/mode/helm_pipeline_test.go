package mode_test

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/provider/helm"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/require"
)

// helmRT adapts a function to an http.RoundTripper, so the real helm provider
// runs against canned registry responses without touching the network.
type helmRT func(*http.Request) (*http.Response, error)

func (f helmRT) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// helmAnon resolves to anonymous access, so the test never reads a real login.
type helmAnon struct{}

func (helmAnon) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

const helmIndexYAML = `apiVersion: v1
entries:
  nginx:
    - version: 18.0.0
      created: "2026-01-02T03:04:05Z"
      digest: aaaa1111
      urls: [https://charts.example.com/nginx-18.0.0.tgz]
    - version: 18.2.0
      created: "2026-02-02T03:04:05Z"
      digest: bbbb2222
      urls: [https://charts.example.com/nginx-18.2.0.tgz]
`

// TestRunResolvesHelmCharts drives the real helm provider end-to-end through the
// pipeline: a classic index.yaml and an OCI tags listing are served by a mock
// transport, and mode.Run rewrites both versions in place. This exercises the
// real index parsing, OCI bearer-token challenge, version selection, and file
// rewrite together - hermetically, with no network.
func TestRunResolvesHelmCharts(t *testing.T) {
	transport := helmRT(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "index.yaml"):
			return body(req, helmIndexYAML), nil
		case strings.Contains(req.URL.Path, "/token"):
			return body(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return helmChallenge(req), nil
			}
			return body(req, `{"tags":["1.0.0","1.2.0","2.0.0"]}`), nil
		}
		return body(req, ""), nil
	})
	provider.Register(helm.New(helm.WithTransport(transport), helm.WithKeychain(helmAnon{})))

	dir := t.TempDir()
	path := filepath.Join(dir, "values.yaml")
	const before = "# clover: provider=helm registry=https://charts.example.com chart=nginx constraint=minor\n" +
		"version: 18.0.0\n" +
		"# clover: provider=helm registry=oci://registry.example.com/charts chart=app constraint=minor\n" +
		"appVersion: 1.0.0\n"
	require.NoError(t, os.WriteFile(path, []byte(before), 0o644))

	_, err := mode.Run(context.Background(), []string{dir}, false)
	require.NoError(t, err)

	const want = "# clover: provider=helm registry=https://charts.example.com chart=nginx constraint=minor\n" +
		"version: 18.2.0\n" +
		"# clover: provider=helm registry=oci://registry.example.com/charts chart=app constraint=minor\n" +
		"appVersion: 1.2.0\n"
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, want, string(got))
}

func body(req *http.Request, content string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(content)),
		Request:    req,
	}
}

func helmChallenge(req *http.Request) *http.Response {
	header := http.Header{}
	header.Set(
		"WWW-Authenticate",
		`Bearer realm="https://registry.example.com/token",service="registry.example.com"`,
	)
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     header,
		Body:       http.NoBody,
		Request:    req,
	}
}
