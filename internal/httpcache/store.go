package httpcache

import (
	"net/http"
	"sync"
)

// Entry is a cached HTTP response, buffered so it can be replayed any number of
// times. The Header is retained in full, so the validators a future
// conditional-request store needs (ETag, Last-Modified) are already present.
type Entry struct {
	Status int
	Header http.Header
	Body   []byte
}

// Store is the cache backend behind the transport. The default is in-memory and
// run-scoped; a disk-backed store for cross-run reuse can be supplied via
// [WithStore] without any change to providers or the transport.
type Store interface {
	Get(key string) (*Entry, bool)
	Set(key string, entry *Entry)
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
