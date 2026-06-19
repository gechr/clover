package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/exec"
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
	prov, err := lookupProvider(m.Provider)
	if err != nil {
		return err
	}
	if _, err = prov.Resource(m.Directive); err != nil {
		return err
	}

	_, located, err := p.locate(m)
	if err != nil {
		return err
	}
	if _, err := rule.Compile(m.Directive, located.Semver()); err != nil {
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
		if err := checkSha256(m); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown value %q", m.Value)
	}

	if _, _, err := p.locate(m); err != nil {
		return err
	}
	return nil
}

// checkSha256 validates a value=sha256 follower offline: the source must be
// known, and the marker must give a way to pick an asset (pattern) or a
// checksums URL.
func checkSha256(m Marker) error {
	switch v, _ := m.Directive.Get(constant.DirectiveSha256Source); v {
	case "", constant.Sha256Auto, constant.Sha256Digest,
		constant.Sha256Checksums, constant.Sha256Download, constant.Sha256Verify:
	default:
		return fmt.Errorf("unknown %s=%q", constant.DirectiveSha256Source, v)
	}

	_, hasURL := m.Directive.Get(constant.DirectiveSha256URL)
	_, hasPattern := m.Directive.Get(constant.DirectivePattern)
	if !hasURL && !hasPattern {
		return fmt.Errorf(
			"value=%s needs %s= (to select an asset) or %s=",
			constant.ValueSha256, constant.DirectivePattern, constant.DirectiveSha256URL,
		)
	}
	return nil
}
