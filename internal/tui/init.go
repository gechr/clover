package tui

import (
	"fmt"
	"strings"

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

// ConfigureInput seeds the second wizard step.
type ConfigureInput struct {
	// AuthSummary is the provider credential summary shown as a note; empty hides
	// it.
	AuthSummary string
	// Path is where the config will be written, shown in the confirmation.
	Path string
	// Exists tailors the confirmation wording for an overwrite.
	Exists bool
	// DefaultVersion pre-fills the required-version input.
	DefaultVersion string
	// ValidateVersion, when set, validates the required-version input live.
	ValidateVersion func(string) error
	// ExcludeOptions are the exclude globs offered for selection.
	ExcludeOptions []string
	// DefaultExcludes are the options preselected from ExcludeOptions.
	DefaultExcludes []string
}

// Settings is what the second wizard step collected.
type Settings struct {
	// RequiredVersion is the version constraint to write, or "" for none.
	RequiredVersion string
	// Excludes are the exclude globs the user kept selected.
	Excludes []string
	// Write is whether the user confirmed writing the config.
	Write bool
}

// Configure shows the provider authentication summary (when non-empty), asks for
// an optional (live-validated) required-version constraint, lets the user pick
// which paths to exclude, and confirms writing the config to in.Path.
func Configure(in ConfigureInput) (Settings, error) {
	settings := Settings{RequiredVersion: in.DefaultVersion, Write: true}

	preselected := make(map[string]bool, len(in.DefaultExcludes))
	for _, glob := range in.DefaultExcludes {
		preselected[glob] = true
	}
	excludeOptions := make([]huh.Option[string], len(in.ExcludeOptions))
	for i, glob := range in.ExcludeOptions {
		excludeOptions[i] = huh.NewOption(glob, glob).Selected(preselected[glob])
	}

	confirmTitle := fmt.Sprintf("Write %s?", in.Path)
	if in.Exists {
		confirmTitle = fmt.Sprintf("%s already exists. Overwrite it?", in.Path)
	}

	versionInput := huh.NewInput().
		Title("Minimum clover version").
		Description("A required-version constraint, or blank for none.").
		Placeholder(">=0.1.0").
		Value(&settings.RequiredVersion)
	if in.ValidateVersion != nil {
		versionInput = versionInput.Validate(in.ValidateVersion)
	}

	fields := make([]huh.Field, 0, 4) //nolint:mnd // note + version + excludes + confirm
	if in.AuthSummary != "" {
		fields = append(fields, huh.NewNote().
			Title("Provider authentication").
			Description(in.AuthSummary))
	}
	fields = append(fields,
		versionInput,
		huh.NewMultiSelect[string]().
			Title("Paths to exclude from scanning").
			Options(excludeOptions...).
			Value(&settings.Excludes),
		huh.NewConfirm().
			Title(confirmTitle).
			Value(&settings.Write),
	)

	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return Settings{}, fmt.Errorf("configure: %w", err)
	}
	settings.RequiredVersion = strings.TrimSpace(settings.RequiredVersion)
	return settings, nil
}
