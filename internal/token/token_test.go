package token_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/token"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

func TestKeyringRoundTrip(t *testing.T) {
	keyring.MockInit()

	store, err := token.New(token.WithDir(t.TempDir()))
	require.NoError(t, err)

	_, ok := store.Get("github.com")
	require.False(t, ok, "nothing stored yet")

	require.NoError(t, store.Set("github.com", "gho_secret"))
	got, ok := store.Get("github.com")
	require.True(t, ok)
	require.Equal(t, "gho_secret", got)

	require.NoError(t, store.Delete("github.com"))
	_, ok = store.Get("github.com")
	require.False(t, ok, "deleted")
}

// TestFileFallback forces the keyring to fail so Set/Get exercise the 0600 file
// path that headless environments rely on.
func TestFileFallback(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrNotFound)

	dir := t.TempDir()
	store, err := token.New(token.WithDir(dir))
	require.NoError(t, err)

	require.NoError(t, store.Set("github.com", "gho_fallback"))

	path := filepath.Join(dir, "github.com")
	info, err := os.Stat(path)
	require.NoError(t, err, "token written to the fallback file")
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "fallback file is owner-only")

	got, ok := store.Get("github.com")
	require.True(t, ok)
	require.Equal(t, "gho_fallback", got)

	require.NoError(t, store.Delete("github.com"))
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
}
