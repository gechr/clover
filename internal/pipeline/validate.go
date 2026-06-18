package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/exec"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/clover/internal/rule"
)

// validate checks each marker offline, then asks the executor - running no-op
// tasks - to flag dangling or cyclic follow edges. The no-ops always succeed, so
// the executor never cascades one marker's failure into another's: each result
// carries the marker's own intrinsic problem, and only genuine graph faults add
// a skip on top.
func (p *plan) validate(ctx context.Context) {
	for i := range p.markers {
		p.results[i].Err = p.check(i)
	}

	tasks := make([]exec.Task, len(p.markers))
	for i, m := range p.markers {
		tasks[i] = exec.Task{
			ID:        m.ID,
			From:      m.From,
			Label:     bareID(m.ID),
			FromLabel: bareID(m.From),
			Run:       func(context.Context) error { return nil },
		}
	}
	for i, r := range exec.Execute(ctx, tasks, p.workers) {
		if r.Skipped && p.results[i].Err == nil {
			p.results[i].Skipped = true
			p.results[i].Reason = r.Reason
		}
	}
}

// check validates marker i without any network access, returning the first
// problem it finds or nil when the marker is well-formed.
func (p *plan) check(i int) error {
	m := p.markers[i]
	if m.IsFollower() {
		return p.checkFollower(m)
	}
	return p.checkProducer(m)
}

// checkProducer verifies a producer names a known provider, builds a valid
// resource, locates an unambiguous version on its target line, and compiles its
// rule - every step the run does before the one thing it skips, the network.
func (p *plan) checkProducer(m Marker) error {
	prov, ok := provider.Get(m.Provider)
	if !ok {
		return fmt.Errorf("unknown provider %q", m.Provider)
	}
	if _, err := prov.Resource(m.Directive); err != nil {
		return err
	}

	_, _, located, err := p.locate(m)
	if err != nil {
		return err
	}
	if _, err := rule.Compile(m.Directive, located.Semver); err != nil {
		return err
	}
	return nil
}

// checkFollower verifies a follower names a producer to follow, requests a
// supported value, and has a target line to rewrite.
func (p *plan) checkFollower(m Marker) error {
	if m.From == "" {
		return errors.New("a follower needs from= naming the producer to follow")
	}
	switch m.Value {
	case "", constant.ValueVersion, constant.ValueCommit:
	case constant.ValueSha256:
		return fmt.Errorf("value=%s is not yet supported", constant.ValueSha256)
	default:
		return fmt.Errorf("unknown value %q", m.Value)
	}

	if _, _, _, err := p.locate(m); err != nil {
		return err
	}
	return nil
}
