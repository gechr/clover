// Package logger centralises configuration of the process-wide clog logger for
// the CLI. main calls Init once before any command runs.
package logger

import (
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gechr/clog"
)

// gradientMax is the elapsed/duration mapped to the end of the colour gradient.
const gradientMax = 20 * time.Second

// Init configures clog for the CLI: structured output to stderr with automatic
// colour and TTY detection. It is the single place CLI logging is set up, so
// levels and other policy can grow here without touching command code.
func Init() {
	clog.SetOutput(clog.Stderr(clog.ColorAuto))

	// Gradient the elapsed/duration fields. Start from the current formats so the
	// env-loaded hyperlink formats survive.
	formats := clog.Default.FieldFormats()
	formats.ElapsedGradientMax = gradientMax
	formats.DurationGradientMax = gradientMax
	clog.SetFieldFormats(formats)

	styles := clog.DefaultStyles()
	styles.Keys["from"] = new(lipgloss.NewStyle().Foreground(lipgloss.Color("1"))) // red
	styles.Keys["to"] = new(lipgloss.NewStyle().Foreground(lipgloss.Color("2")))   // green
	clog.SetStyles(styles)
}
