package command_test

import (
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/stretchr/testify/require"
)

// noCandidateSummary is a summary with one marker that found no candidate.
func noCandidateSummary() mode.Summary {
	return mode.Summary{Outcomes: []mode.Outcome{{
		FileResult: pipeline.FileResult{
			Results: []pipeline.Result{{Err: pipeline.ErrNoCandidate}},
		},
	}}}
}

func TestDeepHintsShallow(t *testing.T) {
	t.Parallel()

	// A shallow run surfaces both truncation (deduplicated) and the no-candidate hint.
	resources, noCandidate := command.DeepHints(
		noCandidateSummary(),
		[]string{"docker.io/library/nginx", "docker.io/library/nginx"},
		false,
	)
	require.Equal(t, []string{"docker.io/library/nginx"}, resources)
	require.True(t, noCandidate)
}

func TestDeepHintsDeepSuggestsNothing(t *testing.T) {
	t.Parallel()

	// A deep run already paged to exhaustion, so it never re-suggests --deep.
	resources, noCandidate := command.DeepHints(
		noCandidateSummary(),
		[]string{"docker.io/library/nginx"},
		true,
	)
	require.Empty(t, resources)
	require.False(t, noCandidate)
}

func TestDeepHintsNoCandidateOnlyWhenMissing(t *testing.T) {
	t.Parallel()

	// Every marker resolved, so only truncation is reported - no no-candidate hint.
	summary := mode.Summary{Outcomes: []mode.Outcome{{
		FileResult: pipeline.FileResult{
			Results: []pipeline.Result{{Resolved: "1.2.3"}},
		},
	}}}
	resources, noCandidate := command.DeepHints(summary, []string{"r"}, false)
	require.Equal(t, []string{"r"}, resources)
	require.False(t, noCandidate)
}
