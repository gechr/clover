package report

import (
	"cmp"
	"fmt"
	"io"

	"github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/display"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report/github"
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
	// OutputGitHub emits GitHub Actions annotations (::error/::warning file=,line=)
	// so changes and problems surface inline on a pull request.
	OutputGitHub Output = "github"
)

// Run renders a run's per-marker outcomes and a closing summary. A dry run logs
// the summary at the Dry level so the output itself signals nothing was written.
// In wide output, markers already up to date are reported too.
func Run(logger *clog.Logger, summary mode.Summary, dryRun bool, output Output) {
	forEach(summary, func(r pipeline.Result) {
		switch {
		case r.Err != nil:
			logger.Error().Line(field.Location, r.Marker.File, line(r)).Err(r.Err).Msg("Failed")
		case r.Skipped:
			logger.Warn().
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.Reason, r.Reason).
				Msg("Skipped")
		case r.Changed:
			msg := "Update applied"
			if dryRun {
				msg = "Update available"
			}
			summarize(logger, dryRun).
				Symbol("⬆️").
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.From, value(r.Current, output)).
				Str(field.To, value(reportTo(r), output)).
				Msg(msg)
		case output == OutputWide:
			logger.Debug().
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.Version, value(r.Current, output)).
				Msg("Already up-to-date")
		}

		// A failed pin verification is non-fatal: it is reported alongside the
		// marker's outcome, not in place of it.
		if r.Verify != nil {
			logger.Error().
				Symbol("🔓").
				Line(field.Location, r.Marker.File, line(r)).
				Err(r.Verify).
				Msg("Pin does not match upstream")
		}
	})

	// Nothing to summarise when no markers were found: the "No Clover comments
	// found" warning already stands on its own.
	if empty(summary) {
		return
	}

	summarize(logger, dryRun).
		Symbol("🏁").
		Int(field.Changed, summary.Changed()).
		Int(field.Skipped, summary.Skipped()).
		Int(field.Failed, summary.Errored()).
		Duration(field.Elapsed, summary.Elapsed).
		Msg("Run complete")
}

// empty reports whether the summary carries no marker results at all.
func empty(summary mode.Summary) bool {
	for _, outcome := range summary.Outcomes {
		if len(outcome.Results) > 0 {
			return false
		}
	}
	return true
}

// Lint renders each invalid or skipped marker and a closing summary. In wide
// output, valid markers are reported too.
func Lint(logger *clog.Logger, summary mode.Summary, output Output) {
	forEach(summary, func(r pipeline.Result) {
		switch {
		case r.Err != nil:
			logger.Error().Line(field.Location, r.Marker.File, line(r)).Err(r.Err).Msg("Invalid")
		case r.Skipped:
			logger.Warn().
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.Reason, r.Reason).
				Msg("Skipped")
		case output == OutputWide:
			logger.Info().Line(field.Location, r.Marker.File, line(r)).Msg("OK")
		}
	})

	logger.Info().
		Int(field.Errored, summary.Errored()).
		Int(field.Skipped, summary.Skipped()).
		Msg("Lint complete")
}

// Format renders the directives that were (or, when checking, would be)
// reformatted, then a closing summary at the Dry level under --check.
func Format(logger *clog.Logger, summary mode.FormatSummary, check bool) {
	for _, file := range summary.Files {
		for _, change := range file.Changes {
			logger.Info().Line(field.Location, file.Path, change.Line+1).Msg("Formatted")
		}
	}

	summarize(logger, check).Int(field.Changed, summary.Changed()).Msg("Format complete")
}

// GitHub writes each actionable result as a GitHub Actions annotation, so a run
// or lint surfaces inline on a pull request: a failure or pin-verification
// mismatch as ::error, a skip or available update as ::warning. file:line locate
// the marker. Clean results emit nothing.
func GitHub(w io.Writer, summary mode.Summary, dryRun bool) {
	forEach(summary, func(r pipeline.Result) {
		switch {
		case r.Err != nil:
			fmt.Fprintln(w, github.Error(r.Marker.File, line(r), r.Err.Error()))
		case r.Skipped:
			fmt.Fprintln(w, github.Warning(r.Marker.File, line(r), "skipped: "+r.Reason))
		case r.Changed:
			verb := "applied"
			if dryRun {
				verb = "available"
			}
			fmt.Fprintln(w, github.Warning(r.Marker.File, line(r),
				fmt.Sprintf("update %s: %s → %s", verb, r.Current, reportTo(r))))
		}
		if r.Verify != nil {
			fmt.Fprintln(w, github.Error(r.Marker.File, line(r),
				"pin does not match upstream: "+r.Verify.Error()))
		}
	})
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

// reportTo is the new value a change reports: the text actually written to the
// line, falling back to the resolved value for a result that did not record one
// (a follower projecting its value verbatim).
func reportTo(r pipeline.Result) string {
	return cmp.Or(r.Written, r.Resolved)
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
