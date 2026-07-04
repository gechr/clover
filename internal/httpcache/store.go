package httpcache

import (
	"net/http"
	"sync"
	"time"
)

// Entry is a cached HTTP response, buffered so it can be replayed any number of
// times. The Header is retained in full, so the validators conditional
// revalidation needs (ETag, Last-Modified) are already present. The JSON tags
// serialize an entry for a disk-backed [Store]. An entry is immutable once
// constructed - replaying clones the Header and only reads the Body.
type Entry struct {
	Status   int         `json:"status"`
	Header   http.Header `json:"header"`
	Body     []byte      `json:"body"`
	StoredAt time.Time   `json:"stored_at"`
}

// Store is the cache backend behind the transport. The default is in-memory and
// run-scoped; a disk-backed store for cross-run reuse can be supplied via
// [WithStore] without any change to providers or the transport.
type Store interface {
	Get(key string) (*Entry, bool)
	Set(key string, entry *Entry)
}

// LayeredStore composes a run-scoped store over a cross-run one: Get checks
// mem first and promotes disk hits into it, Set writes both. The transport
// stays unaware of the layering - it only sees a [Store].
type LayeredStore struct {
	mem  Store
	disk Store
}

// NewLayeredStore returns a store reading through mem to disk.
func NewLayeredStore(mem, disk Store) *LayeredStore {
	return &LayeredStore{mem: mem, disk: disk}
}

// Get returns the entry for key from the first layer holding it.
func (l *LayeredStore) Get(key string) (*Entry, bool) {
	if entry, ok := l.mem.Get(key); ok {
		return entry, true
	}
	entry, ok := l.disk.Get(key)
	if ok {
		l.mem.Set(key, entry)
	}
	return entry, ok
}

// Set stores entry in both layers.
func (l *LayeredStore) Set(key string, entry *Entry) {
	l.mem.Set(key, entry)
	l.disk.Set(key, entry)
}

// MemStore is a concurrency-safe, in-memory [Store]. It lives for as long as the
// transport that holds it, which for a CLI run means the whole run.
type MemStore struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// NewMemStore returns an empty in-memory store.
func NewMemStore() *MemStore {
	return &MemStore{entries: make(map[string]*Entry)}
}

// Get returns the entry for key, if present.
func (m *MemStore) Get(key string) (*Entry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.entries[key]
	return entry, ok
}

// Set stores entry under key.
func (m *MemStore) Set(key string, entry *Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = entry
}
