package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOptionsApply(t *testing.T) {
	t.Parallel()

	cooldown := 7 * 24 * time.Hour

	tests := map[string]struct {
		opt    Option
		assert func(t *testing.T, s settings)
	}{
		"ignore files": {
			opt: WithIgnoreFiles(".gitignore", ".ignore"),
			assert: func(t *testing.T, s settings) {
				t.Helper()
				require.Equal(t, []string{".gitignore", ".ignore"}, s.ignoreFiles)
			},
		},
		"max size": {
			opt: WithMaxSize(4096),
			assert: func(t *testing.T, s settings) {
				t.Helper()
				require.Equal(t, int64(4096), s.maxSize)
			},
		},
		"cooldown": {
			opt: WithCooldown(&cooldown),
			assert: func(t *testing.T, s settings) {
				t.Helper()
				require.Equal(t, &cooldown, s.cooldown)
			},
		},
		"infer": {
			opt: WithInfer(true),
			assert: func(t *testing.T, s settings) {
				t.Helper()
				require.True(t, s.infer)
			},
		},
		"require directive": {
			opt: WithRequireDirective(false),
			assert: func(t *testing.T, s settings) {
				t.Helper()
				require.False(t, s.requireDirective)
			},
		},
		"workers": {
			opt: WithWorkers(5),
			assert: func(t *testing.T, s settings) {
				t.Helper()
				require.Equal(t, 5, s.workers)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, newSettings(tt.opt))
		})
	}
}

func TestNewSettingsClampsWorkers(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, newSettings(WithWorkers(0)).workers, "zero clamps to one")
	require.Equal(t, 1, newSettings(WithWorkers(-3)).workers, "a negative count clamps to one")
	require.False(t, newSettings().now.IsZero(), "the clock defaults to now")
	require.NotNil(t, newSettings().reporter, "the reporter defaults to a no-op")
	require.True(t, newSettings().requireDirective, "require-directive defaults on")
}
