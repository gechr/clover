package forge_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/gechr/clover/internal/forge"
	"github.com/stretchr/testify/require"
)

func TestNormalizeHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"codeberg.org", "codeberg.org", true},
		{"https://git.example.com/", "git.example.com", true},
		{"https://git.example.com", "git.example.com", true},
		{"Git.Example.com:3000", "git.example.com:3000", true},
		{"http://localhost:8080", "localhost:8080", true},
		{"https://git.example.com/org", "", false},  // path
		{"user:pass@git.example.com", "", false},    // userinfo
		{"https://git.example.com/?q=1", "", false}, // query
		{"https://git.example.com/#x", "", false},   // fragment
		{"", "", false},
		{"https://", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, ok := forge.NormalizeHost(tt.in)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSameOrigin(t *testing.T) {
	t.Parallel()

	const start = "https://codeberg.org/api/v1/repos/o/n/tags?page=1"
	require.True(t, forge.SameOrigin(start, "https://codeberg.org/api/v1/repos/o/n/tags?page=2"))
	require.False(t, forge.SameOrigin(start, "https://attacker.example/api/v1/x"))
	require.False(t, forge.SameOrigin(start, "http://codeberg.org/api/v1/x")) // scheme differs
	require.False(t, forge.SameOrigin(start, "://bad"))
}

func TestPKCE(t *testing.T) {
	t.Parallel()

	verifier, challenge, err := forge.PKCE()
	require.NoError(t, err)
	require.Len(t, verifier, 43) // 32 bytes base64url, no padding
	sum := sha256.Sum256([]byte(verifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), challenge)

	// Two calls differ.
	other, _, err := forge.PKCE()
	require.NoError(t, err)
	require.NotEqual(t, verifier, other)
}

func TestRandomState(t *testing.T) {
	t.Parallel()

	a, err := forge.RandomState()
	require.NoError(t, err)
	require.NotEmpty(t, a)
	b, err := forge.RandomState()
	require.NoError(t, err)
	require.NotEqual(t, a, b)
}

func TestNextLink(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	require.Empty(t, forge.NextLink(h))

	h.Set("Link", `<https://x/p2>; rel="next", <https://x/p9>; rel="last"`)
	require.Equal(t, "https://x/p2", forge.NextLink(h))

	h.Set("Link", `<https://x/p9>; rel="last"`)
	require.Empty(t, forge.NextLink(h))
}
