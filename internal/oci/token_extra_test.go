package oci_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/oci"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/stretchr/testify/require"
)

// userKeychain resolves to fixed basic-auth credentials, so the token exchange
// authenticates and a credential fingerprint is computed.
type userKeychain struct {
	username string
	password string
}

func (k userKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.FromConfig(authn.AuthConfig{Username: k.username, Password: k.password}), nil
}

func TestTokenEndpointError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				Header:     http.Header{},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			return challengeResponse(req), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.EqualError(t, err, "oci: token endpoint auth.example.com: 500 Internal Server Error")
}

func TestTokenDecodeError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":`), nil // truncated JSON
		case strings.Contains(req.URL.Path, "/tags/list"):
			return challengeResponse(req), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.EqualError(t, err, "oci: decode token: unexpected EOF")
}

func TestTokenRealmParseError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/tags/list") {
			header := http.Header{}
			header.Set("WWW-Authenticate", `Bearer realm="://bad",service="example.com"`)
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     header,
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.EqualError(t, err, `oci: parse token realm: parse "://bad": missing protocol scheme`)
}

func TestTokenBasicChallengeRetriesUnauthenticated(t *testing.T) {
	t.Parallel()

	var tagCalls int
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/tags/list") {
			tagCalls++
			if tagCalls == 1 {
				header := http.Header{}
				header.Set("WWW-Authenticate", `Basic realm="registry"`)
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Header:     header,
					Body:       http.NoBody,
					Request:    req,
				}, nil
			}
			return jsonResponse(req, `{"tags":["1.0.0"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	tags, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, tags)
	require.Equal(t, 2, tagCalls, "a non-Bearer challenge retries once unauthenticated")
}

func TestTokenStaleBearerRefreshed(t *testing.T) {
	t.Parallel()

	var (
		issued    int
		tokens    = []string{"first", "second"}
		firstUses int
	)
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			tok := tokens[issued]
			issued++
			return jsonResponse(req, fmt.Sprintf(`{"token":%q}`, tok)), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			switch req.Header.Get("Authorization") {
			case "":
				return challengeResponse(req), nil
			case "Bearer first":
				firstUses++
				if firstUses == 1 {
					return jsonResponse(req, `{"tags":["1.0.0"]}`), nil // primes cleanly
				}
				return challengeResponse(req), nil // now stale
			case "Bearer second":
				return jsonResponse(req, `{"tags":["2.0.0"]}`), nil
			}
			return nil, fmt.Errorf("unexpected auth %q", req.Header.Get("Authorization"))
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	client := newClient(transport)
	repo := oci.Repo{Host: "registry.example.com", Repository: "team/img"}

	// Prime the "first" token via a clean challenge exchange.
	tags, _, err := client.Tags(t.Context(), repo, false)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, tags)

	// The cached "first" token is now rejected, forcing a forget and refresh.
	tags, _, err = client.Tags(t.Context(), repo, false)
	require.NoError(t, err)
	require.Equal(t, []string{"2.0.0"}, tags)
	require.Equal(t, 2, issued, "the stale token is forgotten and a new one fetched")
}

func TestTokenKeyReusedAcrossRepos(t *testing.T) {
	t.Parallel()

	var tokenCalls int
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			tokenCalls++
			return jsonResponse(req, `{"token":"shared"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			require.Equal(t, "Bearer shared", req.Header.Get("Authorization"))
			return jsonResponse(req, `{"tags":["1.0.0"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	client := newClient(transport)
	// The shared challenge advertises a fixed scope, so two distinct repos map to
	// the same token key: the second reuses the cached token entry.
	repoA := oci.Repo{Host: "registry.example.com", Repository: "team/a"}
	repoB := oci.Repo{Host: "registry.example.com", Repository: "team/b"}

	_, _, err := client.Tags(t.Context(), repoA, false)
	require.NoError(t, err)
	_, _, err = client.Tags(t.Context(), repoB, false)
	require.NoError(t, err)
	require.Equal(t, 1, tokenCalls, "the token key is shared, so the endpoint is hit once")
}

func TestTokenEmptyValueNotStored(t *testing.T) {
	t.Parallel()

	var tagCalls int
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":""}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			tagCalls++
			if tagCalls == 1 {
				return challengeResponse(req), nil
			}
			return jsonResponse(req, `{"tags":["1.0.0"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	tags, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, tags)
}

func TestTokenWithBasicCredentials(t *testing.T) {
	t.Parallel()

	var sawBasicAuth bool
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			user, pass, ok := req.BasicAuth()
			sawBasicAuth = ok && user == "u" && pass == "p"
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return jsonResponse(req, `{"tags":["1.0.0"]}`), nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	client := oci.New(
		oci.WithTransport(transport),
		oci.WithKeychain(userKeychain{username: "u", password: "p"}),
		oci.WithErrorContext("oci", "authenticate for higher limits"),
	)
	tags, _, err := client.Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.NoError(t, err)
	require.Equal(t, []string{"1.0.0"}, tags)
	require.True(t, sawBasicAuth, "the login credentials authenticate the token exchange")
}

func TestParseChallengeParamWithoutValue(t *testing.T) {
	t.Parallel()

	realm, params := oci.ParseChallenge(`Bearer realm="x",bogus`)
	require.Equal(t, "x", realm)
	_, ok := params["bogus"]
	require.False(t, ok, "a parameter without '=' is skipped")
}

func TestTagsTransportError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial refused")
	})

	_, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.EqualError(
		t,
		err,
		`oci: GET https://registry.example.com/v2/team/img/tags/list?n=100: Get "https://registry.example.com/v2/team/img/tags/list?n=100": dial refused`,
	)
}

func TestTagsListStatusError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Status:     "500 Internal Server Error",
				Header:     http.Header{},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.EqualError(t, err, "oci: list tags for team/img: 500 Internal Server Error")
}

func TestTagsDecodeError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/tags/list"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return jsonResponse(req, `{"tags":`), nil // truncated
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, _, err := newClient(transport).Tags(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		false,
	)
	require.EqualError(t, err, "oci: decode tags: unexpected EOF")
}

func TestDigestNoDigestHeader(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{}, // no Docker-Content-Digest
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, err := newClient(transport).Digest(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img"},
		"1.0.0",
	)
	require.EqualError(t, err, "oci: registry returned no digest for 1.0.0")
}

func TestDigestForPlatformInvalidJSON(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return jsonResponse(req, `{"manifests":`), nil // truncated
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, err := newClient(transport).Digest(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img", Platform: "linux/amd64"},
		"1.0.0",
	)
	require.EqualError(t, err, "oci: parse manifest for 1.0.0: unexpected end of JSON input")
}

func TestDigestForPlatformStatusError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/token"):
			return jsonResponse(req, `{"token":"abc"}`), nil
		case strings.Contains(req.URL.Path, "/manifests/"):
			if req.Header.Get("Authorization") == "" {
				return challengeResponse(req), nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Status:     "404 Not Found",
				Header:     http.Header{},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return nil, fmt.Errorf("no route for %s", req.URL)
	})

	_, err := newClient(transport).Digest(
		t.Context(),
		oci.Repo{Host: "registry.example.com", Repository: "team/img", Platform: "linux/amd64"},
		"1.0.0",
	)
	require.EqualError(t, err, "oci: get manifest for 1.0.0: 404 Not Found")
}

func TestNextLinkMalformedRequestURL(t *testing.T) {
	t.Parallel()

	header := http.Header{}
	header.Set("Link", `</v2/team/img/tags/list?last=z>; rel="next"`)
	require.Empty(t, oci.NextLink(header, "://bad"),
		"a request URL that fails to parse yields no next page")
}
