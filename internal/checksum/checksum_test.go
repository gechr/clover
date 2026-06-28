package checksum_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/checksum"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// server returns a client that answers every request with body, recording the
// requested URL.
func server(t *testing.T, body string, gotURL *string) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if gotURL != nil {
			*gotURL = req.URL.String()
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}
}

const (
	sumA = "cc0e114ec1b458442eeddd8e58916a2c0ecf824abd677c9fc00afa1ab429551f"
	sumB = "223f0f1756e5ec54a35e77e207ac23a7e864ad58c81ab82b77e1da7dbc93c442"
)

func TestFetchByPattern(t *testing.T) {
	t.Parallel()

	body := sumA + "  tool_1.2.3_linux_386.tar.gz\n" +
		sumB + "  tool_1.2.3_linux_amd64.tar.gz\n"

	var gotURL string
	client := server(t, body, &gotURL)
	got, err := checksum.Fetch(t.Context(), client,
		"https://host/v<version>/checksums.txt", "1.2.3", "*linux_amd64*")
	require.NoError(t, err)

	require.Equal(t, sumB, got)
	require.Equal(t, "https://host/v1.2.3/checksums.txt", gotURL, "<version> is substituted")
}

func TestFetchSingleEntryNeedsNoPattern(t *testing.T) {
	t.Parallel()

	got, err := checksum.Fetch(t.Context(), server(t, sumA+"  tool.tar.gz\n", nil),
		"https://host/sum.txt", "1.0.0", "")
	require.NoError(t, err)
	require.Equal(t, sumA, got)
}

func TestFetchBareHash(t *testing.T) {
	t.Parallel()

	got, err := checksum.Fetch(
		t.Context(),
		server(t, sumA+"\n", nil),
		"https://host/sum.txt",
		"1.0.0",
		"",
	)
	require.NoError(t, err)
	require.Equal(t, sumA, got)
}

func TestFetchErrors(t *testing.T) {
	t.Parallel()

	multi := sumA + "  a_linux.tar.gz\n" + sumB + "  b_linux.tar.gz\n"

	_, err := checksum.Fetch(t.Context(), server(t, multi, nil), "u", "1", "")
	require.EqualError(
		t,
		err,
		`checksum: 2 entries, set "pattern" to choose one`,
		"ambiguous without a pattern",
	)

	_, err = checksum.Fetch(t.Context(), server(t, multi, nil), "u", "1", "*windows*")
	require.EqualError(t, err, `checksum: no asset matched pattern "*windows*"`)

	_, err = checksum.Fetch(t.Context(), server(t, multi, nil), "u", "1", "*linux*")
	require.EqualError(t, err, `checksum: pattern "*linux*" matched 2 assets`)

	_, err = checksum.Fetch(t.Context(), server(t, "not a checksum file\n", nil), "u", "1", "")
	require.EqualError(t, err, "checksum: no sha256 entries at u")
}
