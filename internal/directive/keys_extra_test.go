package directive_test

import (
	"testing"

	"github.com/gechr/clover/internal/constant"
	"github.com/gechr/clover/internal/directive"
	"github.com/stretchr/testify/require"
)

func TestCommonKeys(t *testing.T) {
	t.Parallel()

	keys := directive.CommonKeys()
	require.NotEmpty(t, keys)
	require.Contains(t, keys, constant.DirectiveProvider)
	require.Contains(t, keys, constant.RuleConstraint)

	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		require.False(t, seen[key], "duplicate key %q", key)
		seen[key] = true
	}

	require.Equal(t, keys, directive.CommonKeys(), "the vocabulary is stable across calls")
}

func TestPruneUnknownKeys(t *testing.T) {
	t.Parallel()

	dockerKeys := []string{"repository", "registry"}

	tests := map[string]struct {
		pairs       []directive.KV
		provider    []string
		wantPairs   []directive.KV
		wantRemoved []string
	}{
		"all known unchanged": {
			pairs: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "constraint", Value: "minor"},
			},
			wantPairs: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "constraint", Value: "minor"},
			},
		},
		"unknown stripped": {
			pairs: []directive.KV{
				{Key: "provider", Value: "github"},
				{Key: "bogus", Value: "1"},
			},
			wantPairs:   []directive.KV{{Key: "provider", Value: "github"}},
			wantRemoved: []string{"bogus"},
		},
		"provider key preserved": {
			pairs: []directive.KV{
				{Key: "provider", Value: "docker"},
				{Key: "repository", Value: "redis"},
			},
			provider: dockerKeys,
			wantPairs: []directive.KV{
				{Key: "provider", Value: "docker"},
				{Key: "repository", Value: "redis"},
			},
		},
		"multiple unknowns preserve order": {
			pairs: []directive.KV{
				{Key: "zeta", Value: "1"},
				{Key: "provider", Value: "github"},
				{Key: "alpha", Value: "2"},
			},
			wantPairs:   []directive.KV{{Key: "provider", Value: "github"}},
			wantRemoved: []string{"zeta", "alpha"},
		},
		"empty directive": {},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, removed := directive.Directive{Pairs: tc.pairs}.PruneUnknownKeys(tc.provider)
			require.Equal(t, tc.wantPairs, got.Pairs)
			require.Equal(t, tc.wantRemoved, removed)
		})
	}
}
