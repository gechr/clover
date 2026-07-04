package httpcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	xos "github.com/gechr/x/os"
)

const (
	dirPerm   = 0o700
	filePerm  = 0o600
	gcHorizon = 30 * 24 * time.Hour
)

// sensitiveHeaders are stripped before an entry is persisted, so credentials
// and cookies never reach disk.
var sensitiveHeaders = []string{"Authorization", "Proxy-Authenticate", "Set-Cookie"}

// DiskStore is a cross-run [Store] holding one JSON-encoded entry per file,
// named by the cache key (a hex SHA-256, so no escaping is needed). Writes are
// atomic, so concurrent runs race benignly - last write wins. The disk tier is
// best-effort: read and write failures are treated as misses, never errors.
type DiskStore struct {
	dir string
}

// NewDiskStore opens the disk store rooted at dir, creating it if needed, and
// deletes entries not rewritten within the GC horizon.
func NewDiskStore(dir string) (*DiskStore, error) {
	if err := xos.EnsureDir(dir, dirPerm); err != nil {
		return nil, err
	}
	s := &DiskStore{dir: dir}
	s.gc()
	return s, nil
}

// Get returns the entry stored for key. A missing, unreadable, or corrupt file
// is a miss - corrupt files are deleted.
func (s *DiskStore) Get(key string) (*Entry, bool) {
	path := s.path(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		_ = os.Remove(path)
		return nil, false
	}
	return &entry, true
}

// Set persists entry when a later run could serve it again: it must carry a
// validator or positive freshness. Anything else would be pure disk waste.
func (s *DiskStore) Set(key string, entry *Entry) {
	if !entry.revalidatable() {
		if _, ok := lifetime(entry.Header); !ok {
			return
		}
	}
	persisted := *entry
	persisted.Header = entry.Header.Clone()
	for _, name := range sensitiveHeaders {
		persisted.Header.Del(name)
	}
	data, err := json.Marshal(&persisted)
	if err != nil {
		return
	}
	_ = xos.AtomicWrite(s.path(key), data, filePerm)
}

func (s *DiskStore) path(key string) string {
	return filepath.Join(s.dir, key)
}

// gc deletes entries whose file has not been rewritten within the horizon. The
// corpus is small (API pages, capped per entry), so age-based cleanup is
// enough - no LRU.
func (s *DiskStore) gc() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-gcHorizon)
	for _, dirEntry := range entries {
		info, err := dirEntry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(s.dir, dirEntry.Name()))
		}
	}
}
