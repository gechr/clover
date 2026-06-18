package match_test

import (
	"testing"

	"github.com/gechr/clover/internal/match"
	"github.com/stretchr/testify/require"
)

func TestInfer(t *testing.T) {
	const sha = "a0dfaeb072753c3d48cd4df5fdacfd035b2281bf"

	tests := []struct {
		name       string
		path       string
		line       string
		provider   string
		repository string
		ok         bool
	}{
		{
			name:       "reusable workflow pin",
			path:       ".github/workflows/ci.yaml",
			line:       "    uses: gechr/actions/.github/workflows/lint.yaml@" + sha + " # v0.2.0",
			provider:   "github",
			repository: "gechr/actions",
			ok:         true,
		},
		{
			name:       "plain action pin",
			path:       "repo/.github/workflows/ci.yml",
			line:       "      uses: actions/checkout@" + sha + " # v4",
			provider:   "github",
			repository: "actions/checkout",
			ok:         true,
		},
		{
			name: "workflow file but not a uses line",
			path: ".github/workflows/ci.yaml",
			line: "    with:",
			ok:   false,
		},
		{
			name: "uses line outside a workflow file",
			path: "docs/example.md",
			line: "    uses: actions/checkout@" + sha,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, repository, ok := match.Infer(tt.path, tt.line)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.provider, provider)
			require.Equal(t, tt.repository, repository)
		})
	}
}
