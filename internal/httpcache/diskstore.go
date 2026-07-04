package httpcache

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	xos "github.com/gechr/x/os"
)

// sharedDisk is the process-wide cross-run store, layered under every client
// [New] builds once [EnableSharedDisk] has run.
var sharedDisk atomic.Pointer[DiskStore]

// EnableSharedDisk opens the disk store rooted at dir and layers it under every
// client built by [New]. The command layer calls it once, so all providers
// share one handle. Clients already built are covered too - their disk tier is
// a lazy delegate, so enabling works whenever it happens during startup.
func EnableSharedDisk(dir string) error {
	disk, err := NewDiskStore(dir)
	if err != nil {
		return err
	}
	sharedDisk.Store(disk)
	return nil
}

// sharedDiskStore delegates to the process-wide disk store at access time,
// doing nothing until [EnableSharedDisk] has run - providers that build their
// clients before the command layer enables the cache still gain the disk tier.
type sharedDiskStore struct{}

// Get returns the entry for key from the shared disk store, when enabled.
func (sharedDiskStore) Get(key string) (*Entry, bool) {
	disk := sharedDisk.Load()
	if disk == nil {
		return nil, false
	}
	return disk.Get(key)
}

// Set stores entry in the shared disk store, when enabled.
func (sharedDiskStore) Set(key string, entry *Entry) {
	if disk := sharedDisk.Load(); disk != nil {
		disk.Set(key, entry)
	}
}

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

// Set persists entry when a later run could serve it again: a 200 carrying a
// validator or positive freshness. Anything else would be pure disk waste - and
// a negative entry is stable only within a run, so it stays memory-only however
// long its origin-granted lifetime.
func (s *DiskStore) Set(key string, entry *Entry) {
	if entry.Status != http.StatusOK {
		return
	}
	if !entry.revalidatable() {
		if _, ok := lifetime(entry.Header); !ok {
			if entry.FreshUntil.IsZero() {
				return
			}
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
