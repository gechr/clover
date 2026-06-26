package follow

import (
	"fmt"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/registry"
)

// Resolve projects a value from the producer that from names, reading the
// producer's entry out of the run-scoped registry. The executor calls it only
// after the producer's edge has resolved, so a missing producer is a real error
// (a dangling from=, or a cycle the validator should have caught).
//
// when selects the producer's value before the run (old) or after it resolved
// (new, the default). value selects what to project from that candidate: an
// empty value defaults to version; version and commit are direct projections.
// sha256 is fetched by the pipeline (it needs the network), not projected here.
func Resolve(reg *registry.Registry, from, value, when string) (string, error) {
	entry, ok := reg.Get(from)
	if !ok {
		return "", fmt.Errorf("follow: producer %q has not resolved", from)
	}

	candidate, err := selectCandidate(entry, when)
	if err != nil {
		return "", err
	}

	if value == "" {
		value = constant.ValueVersion
	}
	switch value {
	case constant.ValueCommit:
		if candidate.Commit == "" {
			return "", fmt.Errorf("follow: producer %q has no commit", from)
		}
		return candidate.Commit, nil
	case constant.ValueVersion:
		return candidate.Version, nil
	}
	return "", fmt.Errorf("follow: unknown value %q", value)
}

// Candidate returns the producer's selected candidate (old or new per when), for
// a follower that needs more than a projected string - e.g. its release assets
// to source a sha256.
func Candidate(reg *registry.Registry, from, when string) (model.Candidate, error) {
	entry, ok := reg.Get(from)
	if !ok {
		return model.Candidate{}, fmt.Errorf("follow: producer %q has not resolved", from)
	}
	return selectCandidate(entry, when)
}

// selectCandidate picks the producer's old or new candidate.
func selectCandidate(entry registry.Entry, when string) (model.Candidate, error) {
	switch when {
	case "", constant.FollowNew:
		return entry.New, nil
	case constant.FollowOld:
		return entry.Old, nil
	}
	return model.Candidate{}, fmt.Errorf("follow: unknown selector %q", when)
}
