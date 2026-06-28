package pipeline_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// skip=true disables a marker entirely: it is reported as disabled, never
// resolved or rewritten, and - unlike a dependency Skip - it does not fail lint.
func TestRunSkipTrueDisables(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "skipfake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=skipfake repository=x/y skip=true\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Len(t, files[0].Results, 1)

	r := files[0].Results[0]
	require.True(t, r.Disabled)
	require.False(t, r.Changed)
	require.False(t, r.Skipped)
	require.NoError(t, r.Err)
	require.Empty(t, r.Reason, "a bare skip=true records no reason")
}

// skip="reason" disables the marker and records the reason for reporting.
func TestRunSkipReasonDisablesWithReason(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "skipreasonfake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": `# clover: provider=skipreasonfake repository=x/y skip="pinned for CVE-2026-123"` +
			"\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.True(t, r.Disabled)
	require.Equal(t, "pinned for CVE-2026-123", r.Reason)
}

// skip=false leaves the marker enabled: it resolves and rewrites as normal.
func TestRunSkipFalseResolves(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "skipfalsefake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=skipfalsefake repository=x/y skip=false\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.False(t, r.Disabled)
	require.True(t, r.Changed)
	require.Equal(t, "version: 2.0.0", r.NewLine)
}

// An empty skip value is malformed: a directive never carries an empty value.
func TestRunSkipEmptyErrors(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "skipemptyfake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=skipemptyfake repository=x/y skip=\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.False(t, r.Disabled)
	require.EqualError(t, r.Err, `"skip" needs true, false, or a reason`)
}
