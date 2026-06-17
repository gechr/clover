package tui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// SelectProviders asks which upstream providers the project will use, with every
// provider selected by default. The returned slice is the chosen subset.
func SelectProviders(names []string) ([]string, error) {
	selected := make([]string, len(names))
	copy(selected, names)

	options := make([]huh.Option[string], len(names))
	for i, name := range names {
		options[i] = huh.NewOption(name, name).Selected(true)
	}

	field := huh.NewMultiSelect[string]().
		Title("Which providers will this project use?").
		Description("clover checks credentials for the providers you select.").
		Options(options...).
		Value(&selected)

	if err := huh.NewForm(huh.NewGroup(field)).Run(); err != nil {
		return nil, fmt.Errorf("select providers: %w", err)
	}
	return selected, nil
}

// Settings is what the second wizard step collected.
type Settings struct {
	// RequiredVersion is the version constraint to write, or "" for none.
	RequiredVersion string
	// Write is whether the user confirmed writing the config.
	Write bool
}

// Configure shows the provider authentication summary (when non-empty), asks for
// an optional required-version constraint, and confirms writing the config to
// path. exists tailors the confirmation wording for an overwrite.
func Configure(authSummary, path string, exists bool) (Settings, error) {
	settings := Settings{Write: true}

	confirmTitle := fmt.Sprintf("Write %s?", path)
	if exists {
		confirmTitle = fmt.Sprintf("%s already exists. Overwrite it?", path)
	}

	fields := make([]huh.Field, 0, 3) //nolint:mnd // note + input + confirm
	if authSummary != "" {
		fields = append(fields, huh.NewNote().
			Title("Provider authentication").
			Description(authSummary))
	}
	fields = append(fields,
		huh.NewInput().
			Title("Minimum clover version").
			Description("A required-version constraint, or blank for none.").
			Placeholder(">=0.1.0").
			Value(&settings.RequiredVersion),
		huh.NewConfirm().
			Title(confirmTitle).
			Value(&settings.Write),
	)

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return Settings{}, fmt.Errorf("configure: %w", err)
	}
	return settings, nil
}
