package tui

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// Glyphs override huh's defaults: radio buttons for the multi-select prefixes
// and a chevron for the selection cursor.
const (
	radioSelected   = "◉ "
	radioUnselected = "○ "
	selectCursor    = "❯ "
)

// Theme is huh's Charm theme recolored to green shades, with radio-button
// prefixes for multi-select fields. huh resolves it per render against the
// terminal background, so the green ramp - bright (focus accents), deep
// (selected text), muted (descriptions, unselected items) - is picked for the
// detected light or dark mode.
func Theme() huh.ThemeFunc {
	return greenStyles
}

// ProviderTheme is the green theme tuned for the provider-selection step: a lime
// title, a dim-lime description, and a regular-green selected-option row.
func ProviderTheme() huh.ThemeFunc {
	return func(isDark bool) *huh.Styles {
		t := greenStyles(isDark)
		ld := lipgloss.LightDark(isDark)
		lime := ld(lipgloss.Color("#65A30D"), lipgloss.Color("#A3E635"))
		dimLime := ld(lipgloss.Color("#6B7A3E"), lipgloss.Color("#869556"))
		green := ld(lipgloss.Color("#16A34A"), lipgloss.Color("#22C55E"))

		t.Focused.Title = t.Focused.Title.Foreground(lime)
		t.Blurred.Title = t.Blurred.Title.Foreground(lime)
		t.Focused.Description = t.Focused.Description.Foreground(dimLime)
		t.Blurred.Description = t.Blurred.Description.Foreground(dimLime)
		t.Focused.MultiSelectSelector = t.Focused.MultiSelectSelector.Foreground(green)
		t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(green)
		t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(green)

		t.Group.Title = t.Focused.Title
		t.Group.Description = t.Focused.Description
		return t
	}
}

// greenStyles builds the green wizard styles for the given terminal mode.
func greenStyles(isDark bool) *huh.Styles {
	t := huh.ThemeCharm(isDark)
	ld := lipgloss.LightDark(isDark)

	brightGreen := ld(lipgloss.Color("#15803D"), lipgloss.Color("#4ADE80"))
	deepGreen := ld(lipgloss.Color("#047857"), lipgloss.Color("#34D399"))
	mutedGreen := ld(lipgloss.Color("#6B9080"), lipgloss.Color("#5C8374"))

	f := &t.Focused
	f.Title = f.Title.Foreground(brightGreen)
	f.NoteTitle = f.NoteTitle.Foreground(brightGreen)
	f.Directory = f.Directory.Foreground(brightGreen)
	f.Description = f.Description.Foreground(mutedGreen)
	f.SelectSelector = f.SelectSelector.Foreground(brightGreen).SetString(selectCursor)
	f.NextIndicator = f.NextIndicator.Foreground(brightGreen)
	f.PrevIndicator = f.PrevIndicator.Foreground(brightGreen)
	f.MultiSelectSelector = f.MultiSelectSelector.Foreground(brightGreen).SetString(selectCursor)
	f.SelectedOption = f.SelectedOption.Foreground(deepGreen)
	f.SelectedPrefix = lipgloss.NewStyle().Foreground(deepGreen).SetString(radioSelected)
	f.UnselectedPrefix = lipgloss.NewStyle().Foreground(mutedGreen).SetString(radioUnselected)
	f.FocusedButton = f.FocusedButton.Background(brightGreen)
	f.Next = f.FocusedButton
	f.TextInput.Cursor = f.TextInput.Cursor.Foreground(deepGreen)
	f.TextInput.Prompt = f.TextInput.Prompt.Foreground(deepGreen)

	// huh copies Focused into Blurred by value, so mirror the recolored Focused
	// and restore the blurred-only tweaks.
	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.MultiSelectSelector = lipgloss.NewStyle().SetString("  ")
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	return t
}
