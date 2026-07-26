package pipeline_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// producerValueError is the whole message a side value on a producer earns, so a
// regression that drops the pointer to `from=` is visible rather than merely
// still-an-error.
const producerValueError = `"value" sha256 needs "from" naming the producer it ` +
	`belongs to - only a follower projects a side value`

// A side value belongs to a follower, and a producer carrying one used to resolve
// its own upstream and then splice the resolved *version* over the target's hex -
// a line that still parses, still looks pinned, and pins nothing. The refusal is
// what makes that unrepresentable rather than silent.
func TestValidateProducerRejectsSideValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		directive string
		wantErr   string
	}{
		{
			name: "sha256 on a producer",
			directive: "# clover: provider=docker repository=library/alpine " +
				"value=sha256 pattern=x",
			wantErr: producerValueError,
		},
		{
			name:      "commit on a producer",
			directive: "# clover: provider=github repository=cli/cli value=commit",
			wantErr: `"value" commit needs "from" naming the producer it belongs to - ` +
				`only a follower projects a side value`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := write(t, map[string]string{
				"digest.yaml": tt.directive + "\n" +
					"digest: 0000000000000000000000000000000000000000000000000000000000000000\n",
			})

			files, err := pipeline.Validate(context.Background(), []string{dir})
			require.NoError(t, err)
			require.EqualError(t, files[0].Results[0].Err, tt.wantErr)
		})
	}
}

// A run does not call validate - it checks each marker as it resolves it - so the
// refusal has to hold on both paths. This is the half a lint-only test would miss,
// and the half that actually writes the file.
func TestRunProducerRejectsSideValue(t *testing.T) {
	provider.Register(fakeProvider{
		name:       "runsidevalue",
		candidates: []model.Candidate{candidate(t, "1.3.0")},
	})

	const content = "# clover: provider=runsidevalue repository=x/y value=sha256 pattern=x\n" +
		"digest: 0000000000000000000000000000000000000000000000000000000000000000\n"
	dir := write(t, map[string]string{"digest.yaml": content})

	files, err := pipeline.Run(context.Background(), []string{dir})
	require.NoError(t, err)
	require.EqualError(t, files[0].Results[0].Err, producerValueError)

	written, err := os.ReadFile(filepath.Join(dir, "digest.yaml"))
	require.NoError(t, err)
	require.Equal(t, content, string(written), "the refused marker wrote nothing")
}

// value=version is the default spelled out, so it is not a side value and a
// producer may carry it.
func TestValidateProducerAcceptsExplicitVersionValue(t *testing.T) {
	provider.Register(fakeProvider{name: "valueversion"})

	dir := write(t, map[string]string{
		"version.yaml": "# clover: provider=valueversion repository=x/y value=version\n" +
			"version: 1.2.3\n",
	})

	files, err := pipeline.Validate(context.Background(), []string{dir})
	require.NoError(t, err)
	require.NoError(t, files[0].Results[0].Err)
}
