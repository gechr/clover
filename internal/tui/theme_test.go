package tui_test

import (
	"testing"

	"github.com/gechr/clover/internal/tui"
	"github.com/stretchr/testify/require"
)

// TestTheme confirms the green theme resolves to non-nil styles in both terminal
// modes and mirrors the focused title/description onto the group styles.
func TestTheme(t *testing.T) {
	t.Parallel()

	for name, isDark := range map[string]bool{"dark": true, "light": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			styles := tui.Theme()(isDark)
			require.NotNil(t, styles)
			require.Equal(t, styles.Focused.Title, styles.Group.Title)
			require.Equal(t, styles.Focused.Description, styles.Group.Description)
		})
	}
}

// TestProviderTheme confirms the provider-selection theme resolves to non-nil
// styles in both modes and mirrors the focused title/description onto the group.
func TestProviderTheme(t *testing.T) {
	t.Parallel()

	for name, isDark := range map[string]bool{"dark": true, "light": false} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			styles := tui.ProviderTheme()(isDark)
			require.NotNil(t, styles)
			require.Equal(t, styles.Focused.Title, styles.Group.Title)
			require.Equal(t, styles.Focused.Description, styles.Group.Description)
		})
	}
}
