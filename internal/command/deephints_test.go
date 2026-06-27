package command_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

func TestDeepHintsShallowDeduplicates(t *testing.T) {
	t.Parallel()

	nginx := provider.Truncation{
		Resource: "ghcr.io/owner/nginx",
		URL:      "https://ghcr.io/owner/nginx",
	}
	redis := provider.Truncation{
		Resource: "ghcr.io/owner/redis",
		URL:      "https://ghcr.io/owner/redis",
	}

	// Only shallow lookups feed the truncation sink, so every truncated resource
	// warrants a warning, deduplicated.
	resources := command.DeepHints([]provider.Truncation{nginx, nginx, redis})
	require.Equal(t, []provider.Truncation{nginx, redis}, resources)
}

func TestDeepHintsNoTruncationNoHint(t *testing.T) {
	t.Parallel()

	// Nothing truncated, nothing to suggest - a no-candidate failure explains
	// itself in its own error rather than through a separate hint.
	require.Empty(t, command.DeepHints(nil))
}

func TestNoCandidateDeepSelectsTruncatedFailures(t *testing.T) {
	t.Parallel()

	gated := pipeline.Result{Truncated: true, Err: pipeline.ErrNoCandidate}
	wrapped := pipeline.Result{
		Truncated: true,
		Err:       fmt.Errorf("%w: no version satisfies the constraint", pipeline.ErrNoCandidate),
	}
	notTruncated := pipeline.Result{Err: pipeline.ErrNoCandidate}
	otherErr := pipeline.Result{Truncated: true, Err: errors.New("network down")}
	resolved := pipeline.Result{Truncated: true}

	summary := mode.Summary{Outcomes: []mode.Outcome{{
		FileResult: pipeline.FileResult{
			Results: []pipeline.Result{gated, wrapped, notTruncated, otherErr, resolved},
		},
	}}}

	// Only a truncated no-candidate failure can be rescued by --deep: a
	// non-truncated failure (all pages seen), a different error, and a clean
	// resolution are all left alone.
	require.Equal(t, []pipeline.Result{gated, wrapped}, command.NoCandidateDeep(summary))
}
