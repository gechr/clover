package report

import (
	"github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/display"
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
			logger.Error().Line("location", r.Marker.File, line(r)).Err(r.Err).Msg("Failed")
		case r.Skipped:
			logger.Warn().
				Line("location", r.Marker.File, line(r)).
				Str("reason", r.Reason).
				Msg("Skipped")
		case r.Changed:
			msg := "Update applied"
			if dryRun {
				msg = "Update available"
			}
			summarize(logger, dryRun).
				Symbol("⬆️").
				Line("location", r.Marker.File, line(r)).
				Str("from", value(r.Current, output)).
				Str("to", value(r.Resolved, output)).
				Msg(msg)
		case output == OutputWide:
			logger.Debug().
				Line("location", r.Marker.File, line(r)).
				Str("version", value(r.Current, output)).
				Msg("Already up-to-date")
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
			logger.Error().Line("location", r.Marker.File, line(r)).Err(r.Err).Msg("Invalid")
		case r.Skipped:
			logger.Warn().
				Line("location", r.Marker.File, line(r)).
				Str("reason", r.Reason).
				Msg("Skipped")
		case output == OutputWide:
			logger.Info().Line("location", r.Marker.File, line(r)).Msg("OK")
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
			logger.Info().Line("location", file.Path, change.Line+1).Msg("Formatted")
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

// value formats a resolved value for the report: abbreviated under text output
// so a long hash (a commit SHA or sha256 sum) stays readable, shown in full
// under wide output where accounting for the exact value matters.
func value(v string, output Output) string {
	// Trim a leading v only from an actual version - never from a commit SHA,
	// sha256, or other follower-projected value that merely starts with "v".
	if _, err := version.Parse(v); err == nil {
		v = version.RemovePrefix(v)
	}
	if output == OutputWide {
		return v
	}
	return display.Value(v)
}
