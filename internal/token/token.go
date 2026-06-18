// Package token persists the access tokens clover mints (e.g. via the GitHub
// device flow), keyed by host. It prefers the OS keyring (macOS Keychain,
// libsecret, Windows Credential Manager) and falls back to a 0600 file under the
// user config directory when no keyring is available, so headless environments
// still work. The CLI edge owns this; the pure core never sees a credential.
package token

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

// service is the keyring service name all clover tokens live under.
const service = "clover"

// filePerm is the permission for a fallback token file: owner read/write only.
const filePerm = 0o600

// Store reads and writes tokens by host, keyring-first with a file fallback.
type Store struct {
	dir string // base directory for the file fallback
}

// Option configures a [Store].
type Option func(*Store)

// WithDir overrides the file-fallback directory, for tests.
func WithDir(dir string) Option {
	return func(s *Store) { s.dir = dir }
}

// New returns a Store whose file fallback lives under the user config directory
// (e.g. ~/.config/clover/hosts). The directory is created lazily on the first
// write that needs it.
func New(opts ...Option) (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("token: locate config dir: %w", err)
	}
	store := &Store{dir: filepath.Join(dir, service, "hosts")}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
}

// Get returns the token stored for host, trying the keyring then the file
// fallback. Both values are trimmed, and an empty result is reported as a miss
// (false) so an empty entry never shadows a later credential source.
func (s *Store) Get(host string) (string, bool) {
	if tok, err := keyring.Get(service, host); err == nil {
		if tok = strings.TrimSpace(tok); tok != "" {
			return tok, true
		}
	}
	tok, err := os.ReadFile(s.path(host))
	if err != nil {
		return "", false
	}
	if trimmed := strings.TrimSpace(string(tok)); trimmed != "" {
		return trimmed, true
	}
	return "", false
}

// Set stores token for host in the keyring, falling back to a 0600 file when the
// keyring is unavailable (commonly headless Linux without a secret service).
func (s *Store) Set(host, token string) error {
	if err := keyring.Set(service, host, token); err == nil {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("token: create store dir: %w", err)
	}
	if err := os.WriteFile(s.path(host), []byte(token), filePerm); err != nil {
		return fmt.Errorf("token: write token file: %w", err)
	}
	return nil
}

// Delete removes any stored token for host from both the keyring and the file
// fallback. A missing token is not an error.
func (s *Store) Delete(host string) error {
	if err := keyring.Delete(service, host); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("token: delete from keyring: %w", err)
	}
	if err := os.Remove(s.path(host)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("token: remove token file: %w", err)
	}
	return nil
}

// path is the fallback file path for host. The host is sanitised so a value like
// "github.com" maps to a single file rather than a nested path.
func (s *Store) path(host string) string {
	return filepath.Join(s.dir, strings.ReplaceAll(host, string(filepath.Separator), "_"))
}
