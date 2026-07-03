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
	"sync"

	xos "github.com/gechr/x/os"
	"github.com/zalando/go-keyring"
)

// service is the keyring service name all clover tokens live under.
const service = "clover"

// filePerm is the permission for a fallback token file: owner read/write only.
const filePerm = 0o600

// dirPerm is the permission for the fallback store directory: owner-only.
const dirPerm = 0o700

// Store reads and writes tokens by host, keyring-first with a file fallback.
type Store struct {
	mu    sync.RWMutex
	cache map[string]cachedToken
	dir   string // base directory for the file fallback
}

type cachedToken struct {
	token string
	ok    bool
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
	store := &Store{cache: make(map[string]cachedToken), dir: filepath.Join(dir, service, "hosts")}
	for _, opt := range opts {
		opt(store)
	}
	return store, nil
}

// Get returns the token stored for host, trying the keyring then the file
// fallback. Both values are trimmed, and an empty result is reported as a miss
// (false) so an empty entry never shadows a later credential source.
func (s *Store) Get(host string) (string, bool) {
	if token, ok, cached := s.cached(host); cached {
		return token, ok
	}
	if tok, err := keyring.Get(service, host); err == nil {
		if tok = strings.TrimSpace(tok); tok != "" {
			s.cacheSet(host, tok, true)
			return tok, true
		}
	}
	tok, err := os.ReadFile(s.path(host))
	if err != nil {
		s.cacheSet(host, "", false)
		return "", false
	}
	if trimmed := strings.TrimSpace(string(tok)); trimmed != "" {
		s.cacheSet(host, trimmed, true)
		return trimmed, true
	}
	s.cacheSet(host, "", false)
	return "", false
}

// Set stores token for host in the keyring, falling back to a 0600 file when the
// keyring is unavailable (commonly headless Linux without a secret service).
func (s *Store) Set(host, token string) error {
	if err := keyring.Set(service, host, token); err == nil {
		s.cacheSet(host, token, true)
		return nil
	}
	if err := xos.EnsureDir(s.dir, dirPerm); err != nil {
		return fmt.Errorf("token: create store dir: %w", err)
	}
	if err := xos.AtomicWrite(s.path(host), []byte(token), filePerm); err != nil {
		return fmt.Errorf("token: write token file: %w", err)
	}
	s.cacheSet(host, token, true)
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
	s.cacheSet(host, "", false)
	return nil
}

func (s *Store) cached(host string) (string, bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cached, ok := s.cache[host]
	return cached.token, cached.ok, ok
}

func (s *Store) cacheSet(host, token string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = make(map[string]cachedToken)
	}
	trimmed := strings.TrimSpace(token)
	s.cache[host] = cachedToken{token: trimmed, ok: ok && trimmed != ""}
}

// path is the fallback file path for host, sanitised so the host can never
// escape s.dir: both slash forms become "_" and the traversal names "." and ".."
// are defused. So "github.com" maps to one file, and a hostile "../x" cannot
// climb out of the store directory.
func (s *Store) path(host string) string {
	safe := strings.NewReplacer("/", "_", `\`, "_").Replace(host)
	if safe == "" || safe == "." || safe == ".." {
		safe = "_" + safe
	}
	return filepath.Join(s.dir, safe)
}
