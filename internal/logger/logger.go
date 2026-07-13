// Package logger centralizes configuration of the process-wide clog logger for
// the CLI, layered on top of conductor's defaults.
package logger

import (
	"charm.land/lipgloss/v2"
	"github.com/gechr/clog"
	"github.com/gechr/clog/style"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/provider"
	"github.com/gechr/x/terminal"
)

// Configure applies Clover's clog customizations on top of conductor's
// defaults (env prefix, stderr output, duration formats); conductor runs it
// via App.ConfigureLog before any command logs. It is the single place CLI
// logging policy lives, so levels and other tweaks can grow here without
// touching command code.
func Configure() {
	// Pick a quote delimiter per value (" then ' then `) so quoted fields avoid
	// escaping; the default preference order is used.
	clog.SetSmartQuotes(true)

	// Drop empty fields (empty strings, nils) so report lines carry only what
	// matters, while zero counts still show in the summary.
	clog.SetOmitEmpty(true)

	clog.SetNumberFormat(clog.NumberGrouped)

	styles := clog.DefaultStyles()
	styles.Keys[field.From] = new(lipgloss.NewStyle().Foreground(lipgloss.Color("1")))     // red
	styles.Keys[field.To] = new(lipgloss.NewStyle().Foreground(lipgloss.Color("2")))       // green
	styles.Keys[field.Resource] = new(lipgloss.NewStyle().Foreground(lipgloss.Color("3"))) // yellow
	// Dim the quote delimiters while keeping each value's own color, so the
	// quoted text stands out from its surrounding quotes.
	styles.FieldQuote = &style.QuoteStyle{Style: lipgloss.NewStyle().Faint(true), Inherit: true}
	// Tint each provider= value with its own brand color so a run's providers
	// are distinguishable at a glance, adapting to the terminal background.
	styles.KeyValues[field.Provider] = providerStyles()
	clog.SetStyles(styles)
}

// providerStyles builds the per-provider value styling for the provider= field:
// each registered provider's name maps to a style carrying its brand color,
// resolved for the terminal background. A dark background is assumed when
// detection fails (no terminal, or an unresponsive one), matching the common
// case. Registration runs before logging is configured, so the registry is
// already populated here.
func providerStyles() style.KeyValue {
	dark, ok := terminal.IsDark()
	dark = dark || !ok
	values := make(style.ValueMap, len(provider.Names()))
	for _, name := range provider.Names() {
		if p, found := provider.Get(name); found {
			values[name] = new(lipgloss.NewStyle().Foreground(p.Color(dark)))
		}
	}
	return style.KeyValue{Values: values}
}

// SetQuiet drops the CLI log level to warnings and errors only.
func SetQuiet(quiet bool) {
	if quiet {
		clog.SetLevel(clog.LevelWarn)
	}
}

// SetVerbose enables debug-level CLI logs.
func SetVerbose(verbose bool) {
	clog.SetVerbose(verbose)
}
