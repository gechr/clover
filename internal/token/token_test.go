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

// TestKeyringWinsOverFile confirms the keyring is consulted before the file
// fallback, so a stale fallback file never shadows the real credential.
func TestKeyringWinsOverFile(t *testing.T) {
	keyring.MockInit()

	dir := t.TempDir()
	store, err := token.New(token.WithDir(dir))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "github.com"), []byte("filetok"), 0o600))
	require.NoError(t, keyring.Set("clover", "github.com", "keytok"))

	got, ok := store.Get("github.com")
	require.True(t, ok)
	require.Equal(t, "keytok", got, "keyring takes precedence over the file fallback")
}

// TestGetTrimsAndTreatsEmptyAsMiss covers the file-fallback read: a trailing
// newline is trimmed, and a whitespace-only file is reported as absent.
func TestGetTrimsAndTreatsEmptyAsMiss(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrNotFound)

	dir := t.TempDir()
	store, err := token.New(token.WithDir(dir))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "trim.com"), []byte("tok\n"), 0o600))
	got, ok := store.Get("trim.com")
	require.True(t, ok)
	require.Equal(t, "tok", got, "trailing newline trimmed")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "blank.com"), []byte("  \n"), 0o600))
	_, ok = store.Get("blank.com")
	require.False(t, ok, "a whitespace-only token is a miss")
}

// TestPathSanitisesHostSeparators confirms a host with a path separator maps to
// a single file under the store dir rather than a nested path.
func TestPathSanitisesHostSeparators(t *testing.T) {
	keyring.MockInitWithError(keyring.ErrNotFound)

	dir := t.TempDir()
	store, err := token.New(token.WithDir(dir))
	require.NoError(t, err)

	require.NoError(t, store.Set("owner/repo", "tok"))
	got, ok := store.Get("owner/repo")
	require.True(t, ok)
	require.Equal(t, "tok", got)

	_, err = os.Stat(filepath.Join(dir, "owner_repo"))
	require.NoError(t, err, "the separator collapses to a single flat file")
}
