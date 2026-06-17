package registry

import (
	"sync"

	"github.com/gechr/cusp/internal/model"
)

// Registry is the run-scoped store mapping a producer's id to the candidate it
// resolved, so follow markers can reuse a producer's result. It is safe for the
// concurrent producers the executor runs; the follow-edge DAG guarantees a
// producer's Set happens before a follower's Get.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]model.Candidate
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{entries: make(map[string]model.Candidate)}
}

// Set records the candidate a producer resolved under its id.
func (r *Registry) Set(id string, candidate model.Candidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[id] = candidate
}

// Get returns the candidate recorded for id, if any.
func (r *Registry) Get(id string) (model.Candidate, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	candidate, ok := r.entries[id]
	return candidate, ok
}
