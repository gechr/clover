package pipeline

import (
	"context"
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
	// Each marker's offline check writes only its own result slot and reads
	// immutable shared state (the provider registry, its marker, its file lines),
	// so the checks run concurrently, bounded by the worker count.
	exec.Parallel(p.workers, len(p.markers), func(i int) {
		p.results[i].Err = p.check(i)
	})

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
	if err := checkKeys(m); err != nil {
		return err
	}
	if m.Directive.Has(constant.DirectiveTrack) {
		return p.checkTrack(m)
	}
	if m.IsFollower() {
		return p.checkFollower(m)
	}
	return p.checkProducer(m)
}

// checkKeys rejects a marker carrying a key outside the common vocabulary and
// its resolved provider's own keys - a typo or a stale annotation from another
// tool. It is the one validation both lint and run apply up front, so an unknown
// key is caught (logged, its line left untouched) rather than surviving as inert
// configuration that silently changes nothing. A producer whose provider does
// not resolve is deferred entirely - without the provider's keys its own keys
// (repository, chart, …) would look unknown, so the dedicated provider check
// reports the real fault. A follower carries only common keys, so it still gates.
func checkKeys(m Marker) error {
	var providerKeys []string
	if !m.IsFollower() {
		prov, ok := provider.Get(m.Provider)
		if !ok {
			return nil // provider does not resolve; the provider check reports it
		}
		for _, k := range prov.Keys() {
			providerKeys = append(providerKeys, k.Name)
		}
	}
	if m.Sidecar {
		return m.Directive.CheckKeysSidecar(providerKeys)
	}
	return m.Directive.CheckKeys(providerKeys)
}

// trackConflicts are the keys a track= marker may not also carry: the selection
// knobs (no candidate set exists to filter or order), the follower keys (track
// resolves an upstream, it does not project one), the value=sha256 resolver
// keys, and find/replace (track owns its floating-ref locator).
var trackConflicts = []string{
	constant.RuleConstraint,
	constant.RuleInclude,
	constant.RuleExclude,
	constant.RuleAsset,
	constant.RuleBehind,
	constant.RulePrerelease,
	constant.RuleDowngrade,
	constant.RuleTagPrefix,
	constant.DirectiveFrom,
	constant.DirectiveSelect,
	constant.DirectiveValue,
	constant.DirectivePattern,
	constant.DirectiveSha256Source,
	constant.DirectiveSha256URL,
	constant.DirectiveFind,
	constant.DirectiveReplace,
}

// trackPreconditions checks the invariants a track= marker must satisfy
// regardless of provider: it needs an explicit provider (an omitted one means
// follow, but track resolves rather than follows), a ref name or the * infer
// sentinel, and none of the selection, follower, or checksum keys it is mutually
// exclusive with. It is pure, so both lint and the resolve path enforce it.
func trackPreconditions(m Marker) error {
	if m.IsFollower() {
		return fmt.Errorf(
			"%q needs an explicit %q",
			constant.DirectiveTrack, constant.DirectiveProvider,
		)
	}
	if ref, _ := m.Directive.Get(constant.DirectiveTrack); ref == "" {
		return fmt.Errorf(
			"%q needs a ref name or %s to infer it",
			constant.DirectiveTrack, constant.TrackInfer,
		)
	}
	for _, key := range trackConflicts {
		if m.Directive.Has(key) {
			return fmt.Errorf("%q cannot be used with %q", constant.DirectiveTrack, key)
		}
	}
	return nil
}

// checkTrack validates a track= marker offline: its provider-agnostic
// preconditions, plus an explicit provider whose floating-ref pin the line
// carries and which can resolve that pin's secure value.
func (p *plan) checkTrack(m Marker) error {
	if err := trackPreconditions(m); err != nil {
		return err
	}

	prov, err := lookupProvider(m.Provider)
	if err != nil {
		return err
	}
	if _, resourceErr := prov.Resource(m.Directive); resourceErr != nil {
		return resourceErr
	}
	_, located, err := p.locate(m)
	if err != nil {
		return err
	}
	return trackCapable(prov, m.Provider, located.NeedsDigest())
}

// trackCapable reports whether prov can resolve the secure value the located pin
// needs: a content digest (Digester) for a tracked tag, or a branch-head commit
// (Committer) for a tracked branch.
func trackCapable(prov provider.Provider, name string, needsDigest bool) error {
	if needsDigest {
		if _, ok := prov.(provider.Digester); !ok {
			return fmt.Errorf("provider %q cannot resolve a digest for a tracked tag", name)
		}
		return nil
	}
	if _, ok := prov.(provider.Committer); !ok {
		return fmt.Errorf("provider %q cannot resolve a commit for a tracked branch", name)
	}
	return nil
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
		return fmt.Errorf(
			"a follower needs %q naming the producer to follow",
			constant.DirectiveFrom,
		)
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
		return fmt.Errorf("unknown %q value %q", constant.DirectiveSha256Source, v)
	}

	_, hasURL := m.Directive.Get(constant.DirectiveSha256URL)
	_, hasPattern := m.Directive.Get(constant.DirectivePattern)
	if !hasURL && !hasPattern {
		return fmt.Errorf(
			"%q %s needs %q (to select an asset) or %q",
			constant.DirectiveValue,
			constant.ValueSha256,
			constant.DirectivePattern,
			constant.DirectiveSha256URL,
		)
	}
	return nil
}
