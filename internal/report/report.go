package report

import (
	"cmp"
	"fmt"
	"io"

	"github.com/gechr/clive/version"
	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/display"
	"github.com/gechr/clover/internal/httpcache"
	"github.com/gechr/clover/internal/log/field"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report/github"
)

// Run renders a run's per-marker outcomes and a closing summary. A dry run logs
// the summary at the Dry level so the output itself signals nothing was written.
// In wide output, markers already up to date are reported too.
func Run(logger *clog.Logger, summary mode.Summary, dryRun bool, detail output.Mode) {
	forEach(summary, func(r pipeline.Result) {
		switch {
		case r.Err != nil:
			logger.Error().
				Line(field.Location, r.Marker.File, line(r)).
				Err(r.Err).
				Msg("Update check failed")
		case r.Skipped:
			logger.Warn().
				Symbol("📛").
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.Reason, r.Reason).
				Msg("Skipped")
		case r.Disabled:
			logger.Info().
				Symbol("💤").
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.Reason, r.Reason).
				Msg("Disabled")
		case r.Changed:
			msg := "Update applied"
			if dryRun {
				msg = "Update available"
			}
			summarize(logger, dryRun).
				Symbol("⬆️").
				Line(field.Location, r.Marker.File, line(r)).
				Link(field.From, r.CurrentURL, value(r.Current, detail)).
				Link(field.To, r.ResolvedURL, value(reportTo(r), detail)).
				Msg(msg)
		case detail == output.Wide:
			logger.Debug().
				Line(field.Location, r.Marker.File, line(r)).
				Link(field.Version, r.ResolvedURL, value(r.Current, detail)).
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

		// A held pin whose upstream tag moved is advisory: the pin stays put, but
		// the unexpected move (a force-pushed tag) is surfaced so it is not silent.
		if r.Moved != "" {
			logger.Warn().
				Symbol("🔀").
				Line(field.Location, r.Marker.File, line(r)).
				Link(field.From, r.CurrentURL, value(r.Current, detail)).
				Link(field.To, r.ResolvedURL, value(r.Moved, detail)).
				Msg("Pinned upstream tag has moved (pass `--force` to re-pin if safe)")
		}
	})

	// Nothing to summarise when no markers were found: the "No Clover comments
	// found" warning already stands on its own.
	if empty(summary) {
		return
	}

	// Transport accounting, visible under --verbose: how many lookups reached
	// the network and how many the cache layers absorbed. A run that made no
	// requests at all has nothing to account for.
	if stats := httpcache.Snapshot(); stats != (httpcache.Stats{}) {
		logger.Debug().
			Symbol("🌐").
			Int(field.Requests, int(stats.Requests)).
			Int(field.Cached, int(stats.Hits)).
			Int(field.Revalidated, int(stats.Revalidated)).
			Int(field.Coalesced, int(stats.Coalesced)).
			Int(field.Replayed, int(stats.Replayed)).
			Msg("Transport activity")
	}

	summarize(logger, dryRun).
		Symbol("🏁").
		OmitZero(true).
		Int(field.Changed, summary.Changed()).
		Int(field.Skipped, summary.Skipped()).
		Int(field.Disabled, summary.Disabled()).
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
func Lint(logger *clog.Logger, summary mode.Summary, detail output.Mode) {
	forEach(summary, func(r pipeline.Result) {
		switch {
		case r.Err != nil:
			logger.Error().Line(field.Location, r.Marker.File, line(r)).Err(r.Err).Msg("Invalid")
		case r.Skipped:
			logger.Warn().
				Symbol("📛").
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.Reason, r.Reason).
				Msg("Skipped")
		case r.Disabled:
			logger.Info().
				Symbol("💤").
				Line(field.Location, r.Marker.File, line(r)).
				Str(field.Reason, r.Reason).
				Msg("Disabled")
		case detail == output.Wide:
			logger.Info().Symbol("✅").Line(field.Location, r.Marker.File, line(r)).Msg("Valid")
		}
	})

	logger.Info().
		Symbol("🏁").
		OmitZero(true).
		Int(field.Errored, summary.Errored()).
		Int(field.Skipped, summary.Skipped()).
		Int(field.Disabled, summary.Disabled()).
		Msg("Lint complete")
}

// Format renders the directives that were (or, when not writing, would be)
// reformatted, then a closing summary. A written line reads "Formatted" (✨);
// with dry set - under --check or --dry-run - nothing is written, so each line
// reads "Would format" and both it and the summary log at the Dry level.
func Format(logger *clog.Logger, summary mode.FormatSummary, dry bool) {
	msg := "Formatted"
	if dry {
		msg = "Would format"
	}
	for _, file := range summary.Files {
		for _, change := range file.Changes {
			for _, key := range change.Pruned {
				logger.Warn().
					Line(field.Location, file.Path, change.Line+1).
					Str(field.Key, key).
					Msg("Pruned unknown key")
			}
			event := summarize(logger, dry).Line(field.Location, file.Path, change.Line+1)
			if !dry {
				event = event.Symbol("✨")
			}
			event.Msg(msg)
		}
		for _, e := range file.Errors {
			logger.Error().Line(field.Location, file.Path, e.Line+1).Err(e.Err).Msg("Invalid")
		}
	}

	summarize(
		logger,
		dry,
	).Symbol("🏁").
		OmitZero(true).
		Int(field.Changed, summary.Changed()).
		Msg("Format complete")
}

// Annotate renders the annotations clover added (or, in preview, would add),
// then a closing summary. A written line reads "Annotated"/"Reannotated" (🌱/♻️);
// in preview - the default, without --write - nothing is written, so each reads
// "Would add"/"Would update" and both it and the summary log at the Dry level.
func Annotate(logger *clog.Logger, summary mode.AnnotateSummary, write bool) {
	dry := !write
	for _, file := range summary.Files {
		for _, change := range file.Changes {
			event := summarize(logger, dry).
				Str(field.Provider, change.Provider).
				Line(field.Location, file.Path, change.At+1)
			if write {
				event = event.Symbol(annotateSymbol(change.Existing))
			}
			event.Msg(annotateMessage(change.Existing, write))
		}
		// A comment-less target's annotations land in a sidecar, located at the
		// target line they govern and tagged with the sidecar file they live in.
		if file.Sidecar != nil {
			for _, entry := range file.Sidecar.Entries {
				event := summarize(logger, dry).
					Str(field.Provider, entry.Provider).
					Line(field.Location, file.Path, entry.Target+1).
					Path(field.Sidecar, file.Sidecar.Path)
				if write {
					event = event.Symbol(annotateSymbol(entry.Existing))
				}
				event.Msg(annotateMessage(entry.Existing, write))
			}
		}
		for _, skip := range file.Skips {
			logger.Debug().
				Line(field.Location, file.Path, skip.Line+1).
				Str(field.Reason, skip.Reason).
				Path(field.Sidecar, skip.Sidecar).
				Msg("Skipped annotation candidate")
		}
		if file.WriteErr != nil {
			logger.Error().Path(field.Path, file.Path).Err(file.WriteErr).Msg("Write failed")
		}
	}

	summarize(logger, dry).
		Symbol("🏁").
		OmitZero(true).
		Int(field.Added, summary.Added()).
		Int(field.Updated, summary.Updated()).
		Msg("Annotate complete")
}

// annotateSymbol picks the glyph for a written annotation: a sprout for a fresh
// one, recycle for an existing one rewritten under --force.
func annotateSymbol(existing bool) string {
	if existing {
		return "♻️"
	}
	return "🌱"
}

// annotateMessage picks the line message for an annotation by whether it rewrites
// an existing directive and whether it was written.
func annotateMessage(existing, write bool) string {
	switch {
	case existing && write:
		return "Reannotated"
	case existing:
		return "Would reannotate"
	case write:
		return "Annotated"
	default:
		return "Would annotate"
	}
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
		if r.Moved != "" {
			fmt.Fprintln(w, github.Warning(r.Marker.File, line(r),
				"pinned upstream tag has moved: "+r.Current+" → "+r.Moved))
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
func value(v string, detail output.Mode) string {
	// Trim a leading v only from an actual version - never from a commit SHA,
	// sha256, or other follower-projected value that merely starts with "v".
	if _, err := version.Parse(v); err == nil {
		v = version.RemovePrefix(v)
	}
	if detail == output.Wide {
		return v
	}
	return display.Value(v)
}
