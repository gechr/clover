// Package logger centralises configuration of the process-wide clog logger for
// the CLI, layered on top of conductor's defaults.
package logger

import (
	"charm.land/lipgloss/v2"
	"github.com/gechr/clog"
	"github.com/gechr/clog/style"
	"github.com/gechr/clover/internal/log/field"
)

// Configure applies Clover's clog customisations on top of conductor's
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
	styles.Keys[field.From] = new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))) // red
	styles.Keys[field.To] = new(lipgloss.NewStyle().Foreground(lipgloss.Color("2")))   // green
	// Dim the quote delimiters while keeping each value's own colour, so the
	// quoted text stands out from its surrounding quotes.
	styles.FieldQuote = &style.QuoteStyle{Style: lipgloss.NewStyle().Faint(true), Inherit: true}
	clog.SetStyles(styles)
}

// SetVerbose enables debug-level CLI logs.
func SetVerbose(verbose bool) {
	clog.SetVerbose(verbose)
}
