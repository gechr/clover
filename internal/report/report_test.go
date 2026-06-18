package report_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/stretchr/testify/require"
)

// summary wraps results into a one-file mode.Summary.
func summary(results ...pipeline.Result) mode.Summary {
	return mode.Summary{
		Outcomes: []mode.Outcome{{FileResult: pipeline.FileResult{Results: results}}},
	}
}

func result(file string, target int) pipeline.Result {
	return pipeline.Result{Marker: pipeline.Marker{File: file, Target: target}}
}

func TestRun(t *testing.T) {
	updated := result("x/app.txt", 1)
	updated.Current, updated.Resolved, updated.Changed = "1.2.0", "1.3.0", true
	skipped := result("x/b.txt", 3)
	skipped.Skipped, skipped.Reason = true, "dep failed"

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(updated, skipped), false, report.OutputText)

	require.Equal(t,
		"INF ⬆️ Update applied location=x/app.txt:2 from=1.2.0 to=1.3.0\n"+
			"WRN ⚠️ Skipped location=x/b.txt:4 reason=\"dep failed\"\n"+
			"INF 🏁 Run complete changed=1 skipped=1 failed=0\n",
		buf.String(),
	)
}

// TestRunAbbreviatesHashValues confirms text output shortens long commit/sha256
// values to a head…tail form so they do not dominate the line.
func TestRunAbbreviatesHashValues(t *testing.T) {
	const (
		oldSHA = "0123456789abcdef0123456789abcdef01234567"
		newSHA = "fedcba9876543210fedcba9876543210fedcba98"
	)
	pinned := result("ci.yml", 0)
	pinned.Current, pinned.Resolved, pinned.Changed = oldSHA, newSHA, true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(pinned), false, report.OutputText)

	require.Equal(t,
		"INF ⬆️ Update applied location=ci.yml:1 from=012345…234567 to=fedcba…dcba98\n"+
			"INF 🏁 Run complete changed=1 skipped=0 failed=0\n",
		buf.String(),
	)
}

// TestRunWideShowsFullHash confirms wide output keeps the exact value, since
// wide exists to account for the precise resolution.
func TestRunWideShowsFullHash(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	pinned := result("ci.yml", 0)
	pinned.Current, pinned.Resolved, pinned.Changed = "1.0.0", sha, true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(pinned), false, report.OutputWide)

	require.Equal(t,
		"INF ⬆️ Update applied location=ci.yml:1 from=1.0.0 to="+sha+"\n"+
			"INF 🏁 Run complete changed=1 skipped=0 failed=0\n",
		buf.String(),
	)
}

func TestRunDryLogsSummaryAtDryLevel(t *testing.T) {
	updated := result("app.txt", 0)
	updated.Current, updated.Resolved, updated.Changed = "1.0.0", "2.0.0", true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(updated), true, report.OutputText)

	require.Equal(t,
		"DRY ⬆️ Update available location=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"DRY 🏁 Run complete changed=1 skipped=0 failed=0\n",
		buf.String(),
	)
}

// TestFormatUsesFullPathForHyperlink confirms format lines carry the full path
// (clog renders it as a file:line hyperlink on a TTY).
func TestFormatUsesFullPathForHyperlink(t *testing.T) {
	fs := mode.FormatSummary{Files: []mode.FormatFile{
		{Path: "dir/app.txt", Changes: []mode.FormatChange{{Line: 4}}},
	}}

	var buf bytes.Buffer
	report.Format(clog.NewWriter(&buf), fs, false)

	require.Equal(t,
		"INF ℹ️ Formatted location=dir/app.txt:5\n"+
			"INF ℹ️ Format complete changed=1\n",
		buf.String(),
	)
}

func TestLint(t *testing.T) {
	bad := result("a.txt", 0)
	bad.Err = errors.New("boom")

	var buf bytes.Buffer
	report.Lint(clog.NewWriter(&buf), summary(bad), report.OutputText)

	require.Equal(t,
		"ERR ❌ Invalid location=a.txt:1 error=boom\n"+
			"INF ℹ️ Lint complete errored=1 skipped=0\n",
		buf.String(),
	)
}

func TestRunWideReportsUpToDate(t *testing.T) {
	updated := result("app.txt", 0)
	updated.Current, updated.Resolved, updated.Changed = "1.0.0", "2.0.0", true
	steady := result("app.txt", 2)
	steady.Current = "1.5.0" // resolved, already up to date

	var buf bytes.Buffer
	logger := clog.NewWriter(&buf)
	logger.SetLevel(clog.LevelDebug) // the up-to-date line is logged at debug
	report.Run(logger, summary(updated, steady), false, report.OutputWide)

	require.Equal(t,
		"INF ⬆️ Update applied location=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"DBG 🐞 Already up-to-date location=app.txt:3 version=1.5.0\n"+
			"INF 🏁 Run complete changed=1 skipped=0 failed=0\n",
		buf.String(),
	)
}

func TestLintWideReportsValid(t *testing.T) {
	bad := result("a.txt", 0)
	bad.Err = errors.New("boom")
	ok := result("b.txt", 1) // valid: no error, not skipped

	var buf bytes.Buffer
	report.Lint(clog.NewWriter(&buf), summary(bad, ok), report.OutputWide)

	require.Equal(t,
		"ERR ❌ Invalid location=a.txt:1 error=boom\n"+
			"INF ℹ️ OK location=b.txt:2\n"+
			"INF ℹ️ Lint complete errored=1 skipped=0\n",
		buf.String(),
	)
}

func TestFormatCheckLogsAtDryLevel(t *testing.T) {
	fs := mode.FormatSummary{Files: []mode.FormatFile{
		{Path: "d/app.txt", Changes: []mode.FormatChange{{Line: 0}}},
	}}

	var buf bytes.Buffer
	report.Format(clog.NewWriter(&buf), fs, true)

	require.Equal(t,
		"INF ℹ️ Formatted location=d/app.txt:1\n"+
			"DRY 🚧 Format complete changed=1\n",
		buf.String(),
	)
}
