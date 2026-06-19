package version_test

import (
	"testing"
	"time"

	"github.com/gechr/clover/internal/version"
	"github.com/stretchr/testify/require"
)

// TestSelectObserverReportsSkipReasons checks that WithObserver fires once per
// rejected candidate with the reason that dropped it, and never for the
// candidate that is selected.
func TestSelectObserverReportsSkipReasons(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := mustParse(t, "1.2.0")
	minor, err := version.NewConstraint("minor", current)
	require.NoError(t, err)

	tests := []struct {
		name    string
		current *version.Version
		cands   []cand
		opts    []version.Option
		want    map[string]version.Reason
	}{
		{
			name:  "eligible is never reported",
			cands: candidates("1.2.0", "1.3.0"),
			want:  map[string]version.Reason{},
		},
		{
			name:  "unparsable tag",
			cands: candidates("1.3.0", "not-a-version"),
			want:  map[string]version.Reason{"not-a-version": version.ReasonUnparsable},
		},
		{
			name:  "dropped by exclude",
			cands: candidates("1.3.0", "1.4.0"),
			opts:  []version.Option{version.WithExclude(contains("1.4"))},
			want:  map[string]version.Reason{"1.4.0": version.ReasonFiltered},
		},
		{
			name:  "prerelease not allowed",
			cands: candidates("1.3.0", "1.4.0-rc1"),
			want:  map[string]version.Reason{"1.4.0-rc1": version.ReasonPrerelease},
		},
		{
			name:  "younger than cooldown",
			cands: []cand{{tag: "1.3.0"}, {tag: "1.4.0", published: now}},
			opts:  []version.Option{version.WithCooldown(72 * time.Hour), version.WithNow(now)},
			want:  map[string]version.Reason{"1.4.0": version.ReasonCooldown},
		},
		{
			name:    "outside the constraint",
			current: current,
			cands:   candidates("1.3.0", "2.0.0"),
			opts:    []version.Option{version.WithConstraint(minor)},
			want:    map[string]version.Reason{"2.0.0": version.ReasonConstraint},
		},
		{
			name:    "downgrade disallowed",
			current: current,
			cands:   candidates("1.3.0", "1.1.0"),
			want:    map[string]version.Reason{"1.1.0": version.ReasonDowngrade},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := map[string]version.Reason{}
			opts := make([]version.Option, 0, len(tc.opts)+1)
			opts = append(opts, tc.opts...)
			opts = append(opts, version.WithObserver(func(tag string, r version.Reason) {
				got[tag] = r
			}))

			_, ok := version.Select(tc.current, tc.cands, attrsOf, opts...)
			require.True(t, ok, "an eligible candidate should still be selected")
			require.Equal(t, tc.want, got)
		})
	}
}
