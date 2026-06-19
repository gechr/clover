// Package logger centralises configuration of the process-wide clog logger for
// the CLI. main calls Init once before any command runs.
package logger

import (
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gechr/clog"
	"github.com/gechr/clog/style"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/x/human"
)

// gradientMax is the elapsed/duration mapped to the end of the colour gradient.
const gradientMax = 20 * time.Second

// Init configures clog for the CLI: structured output to stderr with automatic
// colour and TTY detection. It is the single place CLI logging is set up, so
// levels and other policy can grow here without touching command code.
func Init() {
	clog.SetOutput(clog.Stderr(clog.ColorAuto))

	// Pick a quote delimiter per value (" then ' then `) so quoted fields avoid
	// escaping; the default preference order is used.
	clog.SetSmartQuotes(true)

	// Drop empty fields (empty strings, nils) so report lines carry only what
	// matters, while zero counts still show in the summary.
	clog.SetOmitEmpty(true)

	// Start from the current formats so the env-loaded hyperlink formats survive.
	formats := clog.Default.FieldFormats()
	formats.DurationFormat = human.FormatDuration
	formats.ElapsedGradientMax = gradientMax
	formats.DurationGradientMax = gradientMax
	clog.SetFieldFormats(formats)

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
