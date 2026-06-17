package report

import (
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
)

// Output is the detail level of a report. Its string values double as the CLI's
// --output enum.
type Output string

const (
	// OutputText is the concise default: only changes and problems, plus a
	// summary.
	OutputText Output = "text"
	// OutputWide additionally reports every marker that was already up to date or
	// valid, so the output accounts for all of them.
	OutputWide Output = "wide"
)

// Run renders a run's per-marker outcomes and a closing summary. A dry run logs
// the summary at the Dry level so the output itself signals nothing was written.
// In wide output, markers already up to date are reported too.
func Run(logger *clog.Logger, summary mode.Summary, dryRun bool, output Output) {
	forEach(summary, func(r pipeline.Result) {
		switch {
		case r.Err != nil:
			logger.Error().Line("at", r.Marker.File, line(r)).Err(r.Err).Msg("Failed")
		case r.Skipped:
			logger.Warn().Line("at", r.Marker.File, line(r)).Str("reason", r.Reason).Msg("Skipped")
		case r.Changed:
			logger.Info().
				Line("at", r.Marker.File, line(r)).
				Str("from", r.Current).
				Str("to", r.Resolved).
				Msg("Updated")
		case output == OutputWide:
			logger.Info().
				Line("at", r.Marker.File, line(r)).
				Str("version", r.Current).
				Msg("Up to date")
		}
	})

	summarize(logger, dryRun).
		Int("changed", summary.Changed()).
		Int("skipped", summary.Skipped()).
		Int("failed", summary.Errored()).
		Msg("Run complete")
}

// Lint renders each invalid or skipped marker and a closing summary. In wide
// output, valid markers are reported too.
func Lint(logger *clog.Logger, summary mode.Summary, output Output) {
	forEach(summary, func(r pipeline.Result) {
		switch {
		case r.Err != nil:
			logger.Error().Line("at", r.Marker.File, line(r)).Err(r.Err).Msg("Invalid")
		case r.Skipped:
			logger.Warn().Line("at", r.Marker.File, line(r)).Str("reason", r.Reason).Msg("Skipped")
		case output == OutputWide:
			logger.Info().Line("at", r.Marker.File, line(r)).Msg("OK")
		}
	})

	logger.Info().
		Int("errored", summary.Errored()).
		Int("skipped", summary.Skipped()).
		Msg("Lint complete")
}

// Format renders the directives that were (or, when checking, would be)
// reformatted, then a closing summary at the Dry level under --check.
func Format(logger *clog.Logger, summary mode.FormatSummary, check bool) {
	for _, file := range summary.Files {
		for _, change := range file.Changes {
			logger.Info().Line("at", file.Path, change.Line+1).Msg("Reformatted")
		}
	}

	summarize(logger, check).Int("changed", summary.Changed()).Msg("Format complete")
}

// forEach calls fn for every marker result across the summary's files, in order.
func forEach(summary mode.Summary, fn func(pipeline.Result)) {
	for _, outcome := range summary.Outcomes {
		for _, result := range outcome.Results {
			fn(result)
		}
	}
}

// summarize selects the summary level: Dry (🚧) when nothing was written, else
// Info.
func summarize(logger *clog.Logger, dry bool) *clog.Event {
	if dry {
		return logger.Dry()
	}
	return logger.Info()
}

// line is a result's 1-based target line, for a clog file:line hyperlink.
func line(r pipeline.Result) int {
	return r.Marker.Target + 1
}
