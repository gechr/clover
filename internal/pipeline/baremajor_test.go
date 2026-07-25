package pipeline_test

import (
	"context"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// semverProvider declares the BareMajorer capability, standing in for a toolchain
// provider whose upstream never publishes a calendar tag.
type semverProvider struct{ fakeProvider }

func (semverProvider) BareMajor() {}

// A bare single-number pin (node-version: 20) is ambiguous between a
// major-precision pin and a calendar tag, and selection's scheme guard reads it
// as the latter by default - so every dotted candidate is rejected and the marker
// resolves to nothing. Two independent things lift that guard: a mise file, where
// the shape carries the meaning whatever provider resolves it, and a BareMajorer
// provider, whose upstream has no calendar tags to guard against.
func TestBareMajorPin(t *testing.T) {
	t.Run("a BareMajorer resolves a bare pin outside a mise file", func(t *testing.T) {
		provider.Register(semverProvider{fakeProvider{
			name:       "semvered",
			candidates: []model.Candidate{candidate(t, "20.11.0"), candidate(t, "24.3.0")},
		}})

		dir := write(t, map[string]string{
			".github/workflows/ci.yml": "# clover: provider=semvered\nnode-version: 20\n",
		})
		files, err := pipeline.Run(context.Background(), []string{dir})
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.Len(t, files[0].Results, 1)
		// The pin keeps its precision: a bare major resolves to the newest 24.x and
		// is written back as a bare major, so 20 becomes 24 rather than 24.3.0.
		require.Equal(t, "24.3.0", files[0].Results[0].Resolved)
		require.Equal(t, "24", files[0].Results[0].Written)
	})

	t.Run("a provider without the capability keeps the calendar-tag guard", func(t *testing.T) {
		provider.Register(fakeProvider{
			name:       "calendared",
			candidates: []model.Candidate{candidate(t, "20.11.0"), candidate(t, "24.3.0")},
		})

		dir := write(t, map[string]string{
			".github/workflows/ci.yml": "# clover: provider=calendared\nnode-version: 20\n",
		})
		files, err := pipeline.Run(context.Background(), []string{dir})
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.Len(t, files[0].Results, 1)
		require.Empty(
			t,
			files[0].Results[0].Written,
			"a dotted candidate may not replace a calendar tag",
		)
	})

	t.Run("a mise file lifts the guard for any provider", func(t *testing.T) {
		provider.Register(fakeProvider{
			name:       "calendared2",
			candidates: []model.Candidate{candidate(t, "20.11.0"), candidate(t, "24.3.0")},
		})

		dir := write(t, map[string]string{
			"mise.toml": "# clover: provider=calendared2\nnode = \"20\"\n",
		})
		files, err := pipeline.Run(context.Background(), []string{dir})
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.Len(t, files[0].Results, 1)
		require.Equal(t, "24.3.0", files[0].Results[0].Resolved)
		require.Equal(t, "24", files[0].Results[0].Written)
	})
}
