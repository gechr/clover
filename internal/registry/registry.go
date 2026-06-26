package registry

import (
	"sync"

	"github.com/gechr/clover/internal/model"
)

// Entry pairs a producer's value before the run with the value it resolved, so a
// follow marker can request either the old or the new value.
type Entry struct {
	Old model.Candidate // the value currently in the file, before the run
	New model.Candidate // the value the producer resolved
}

// Registry is the run-scoped store mapping a producer's id to its resolved
// entry, so follow markers can reuse a producer's result. It is safe for the
// concurrent producers the executor runs; the follow-edge DAG guarantees a
// producer's Set happens before a follower's Get.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{entries: make(map[string]Entry)}
}

// Set records the entry a producer resolved under its id.
func (r *Registry) Set(id string, entry Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[id] = entry
}

// Get returns the entry recorded for id, if any.
func (r *Registry) Get(id string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[id]
	return entry, ok
}
