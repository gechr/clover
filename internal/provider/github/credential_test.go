package github_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/provider/github"
	"github.com/stretchr/testify/require"
)

// stubStore is a tokenStore returning a fixed value.
type stubStore struct {
	token string
	ok    bool
}

func (s stubStore) Get(string) (string, bool) { return s.token, s.ok }

// TestAuthenticateWithStoredToken confirms a stored token satisfies
// Authenticate; the store hit short-circuits the chain, so it is hermetic
// regardless of the machine's gh credentials.
func TestAuthenticateWithStoredToken(t *testing.T) {
	t.Setenv("CLOVER_GITHUB_TOKEN", "") // ensure the env rung is empty
	provider := github.New(github.WithStore(stubStore{token: "stored", ok: true}))
	require.NoError(t, provider.Authenticate(context.Background()))
}

func TestAuthHint(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		"for higher rate limits and private repositories, "+
			"run `clover login github` or set `CLOVER_GITHUB_TOKEN`",
		github.New().AuthHint(),
	)
}
