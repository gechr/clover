package pipeline

import (
	"context"
	"reflect"
	"sync"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/provider"
)

// discoveryKey identifies one effective lookup: the provider, the validated
// resource it will fetch, and every hint that shapes the response. Markers
// whose keys are equal would receive identical candidate sets, so the plan
// fetches once and shares the result across them.
type discoveryKey struct {
	provider string
	resource provider.Resource
	hints    provider.Hints
}

// discovery is one memoized lookup. The first caller fetches under once while
// duplicates block, then every caller shares the outcome: the candidates, the
// error, and the truncations noted during the fetch, recorded so each caller
// can replay them to its own sink.
type discovery struct {
	once        sync.Once
	candidates  []model.Candidate
	truncations []provider.Truncation
	err         error
}

// discover memoizes prov.Discover by effective lookup key for the run, so
// duplicate markers cost one upstream fetch (and one response parse) instead
// of one each. Errors are memoized too - a failed lookup fails every duplicate
// without retrying. The shared candidates are immutable per the [provider.Provider]
// contract; selection copies survivors rather than reordering its input. A
// resource that cannot serve as a map key resolves directly.
func (p *plan) discover(
	ctx context.Context,
	prov provider.Provider,
	resource provider.Resource,
) ([]model.Candidate, error) {
	if resource == nil || !reflect.TypeOf(resource).Comparable() {
		return prov.Discover(ctx, resource)
	}
	key := discoveryKey{
		provider: prov.Name(),
		resource: resource,
		hints:    provider.HintsFrom(ctx),
	}
	p.discoveryMu.Lock()
	d, ok := p.discoveries[key]
	if !ok {
		d = new(discovery)
		p.discoveries[key] = d
	}
	p.discoveryMu.Unlock()

	d.once.Do(func() {
		// Record truncations instead of forwarding them: the fetch runs once,
		// but every caller must observe its truncation as if the lookup were
		// its own. The sink may be called concurrently, hence the lock.
		var mu sync.Mutex
		fetch := provider.WithTruncationSink(ctx, func(t provider.Truncation) {
			mu.Lock()
			d.truncations = append(d.truncations, t)
			mu.Unlock()
		})
		d.candidates, d.err = prov.Discover(fetch, resource)
	})
	// Replay the recorded truncations to this marker's own sink, so a shared
	// lookup feeds the blanket hint or the per-marker flag exactly as a private
	// one would.
	for _, t := range d.truncations {
		provider.NoteTruncated(ctx, t.Resource, t.URL)
	}
	return d.candidates, d.err
}
