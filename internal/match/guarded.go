package match

// Guarded is a rewriter wrapper that first requires a find/replace-style guard
// to match the line, then delegates the actual location and rendering to another
// rewriter. It is useful when a sidecar's jq locator selects the line, but a
// find pattern should still fail loud if the line has drifted to another source.
type Guarded struct {
	guard FindReplace
	inner Rewriter
}

// NewGuarded returns a rewriter guarded by find. The guard is match-only; render
// behavior comes entirely from inner.
func NewGuarded(find string, inner Rewriter) (Guarded, error) {
	guard, err := NewFindReplace(find, "")
	if err != nil {
		return Guarded{}, err
	}
	return Guarded{guard: guard, inner: inner}, nil
}

// Locate verifies the guard matches before delegating to the inner rewriter.
func (g Guarded) Locate(line string) (Location, error) {
	if _, err := g.guard.Locate(line); err != nil {
		return nil, err
	}
	return g.inner.Locate(line)
}
