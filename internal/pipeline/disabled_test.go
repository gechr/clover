package pipeline_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// disabled=true disables a marker entirely: it is reported as disabled, never
// resolved or rewritten, and - unlike a dependency Skip - it does not fail lint.
func TestRunDisabledTrueDisables(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "disabledfake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=disabledfake repository=x/y disabled=true\nversion: 1.0.0\n",
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
	require.Empty(t, r.Reason, "a bare disabled=true records no reason")
}

// disabled="reason" disables the marker and records the reason for reporting.
func TestRunDisabledReasonDisablesWithReason(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "disabledreasonfake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": `# clover: provider=disabledreasonfake repository=x/y disabled="pinned for CVE-2026-123"` +
			"\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.True(t, r.Disabled)
	require.Equal(t, "pinned for CVE-2026-123", r.Reason)
}

// disabled=false leaves the marker enabled: it resolves and rewrites as normal.
func TestRunDisabledFalseResolves(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "disabledfalsefake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=disabledfalsefake repository=x/y disabled=false\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.False(t, r.Disabled)
	require.True(t, r.Changed)
	require.Equal(t, "version: 2.0.0", r.NewLine)
}

// An empty disabled value is malformed: a directive never carries an empty value.
func TestRunDisabledEmptyErrors(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "disabledemptyfake",
		candidates: []model.Candidate{candidate(t, "2.0.0")},
	})

	dir := write(t, map[string]string{
		"a.txt": "# clover: provider=disabledemptyfake repository=x/y disabled=\nversion: 1.0.0\n",
	})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	r := files[0].Results[0]
	require.False(t, r.Disabled)
	require.EqualError(t, r.Err, `"disabled" needs true, false, or a reason`)
}
