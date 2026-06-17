// Package logger centralises configuration of the process-wide clog logger for
// the CLI. main calls Init once before any command runs.
package logger

import "github.com/gechr/clog"

// Init configures clog for the CLI: structured output to stderr with automatic
// colour and TTY detection. It is the single place CLI logging is set up, so
// levels and other policy can grow here without touching command code.
func Init() {
	clog.SetOutput(clog.Stderr(clog.ColorAuto))
}
