package follow

import (
	"fmt"

	"github.com/gechr/cusp/internal/constant"
	"github.com/gechr/cusp/internal/registry"
)

// Resolve projects a value from the producer that from names, reading the
// producer's resolved candidate out of the run-scoped registry. The executor
// calls it only after the producer's edge has resolved, so a missing producer
// is a real error (a dangling from=, or a cycle the validator should have
// caught). An empty value defaults to the version.
//
// version and commit are direct projections; sha256 is fetched using the
// producer's version and is not yet implemented here.
func Resolve(reg *registry.Registry, from, value string) (string, error) {
	candidate, ok := reg.Get(from)
	if !ok {
		return "", fmt.Errorf("follow: producer %q has not resolved", from)
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
	case constant.ValueSha256:
		return "", fmt.Errorf("follow: value=%s is not yet supported", constant.ValueSha256)
	case constant.ValueVersion:
		return candidate.Version, nil
	}
	return "", fmt.Errorf("follow: unknown value %q", value)
}
