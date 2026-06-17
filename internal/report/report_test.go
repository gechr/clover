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
		"INF ℹ️ Updated at=x/app.txt:2 from=1.2.0 to=1.3.0\n"+
			"WRN ⚠️ Skipped at=x/b.txt:4 reason=\"dep failed\"\n"+
			"INF ℹ️ Run complete changed=1 skipped=1 failed=0\n",
		buf.String(),
	)
}

func TestRunDryLogsSummaryAtDryLevel(t *testing.T) {
	updated := result("app.txt", 0)
	updated.Current, updated.Resolved, updated.Changed = "1.0.0", "2.0.0", true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(updated), true, report.OutputText)

	require.Equal(t,
		"INF ℹ️ Updated at=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"DRY 🚧 Run complete changed=1 skipped=0 failed=0\n",
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
		"INF ℹ️ Reformatted at=dir/app.txt:5\n"+
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
		"ERR ❌ Invalid at=a.txt:1 error=boom\n"+
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
	report.Run(clog.NewWriter(&buf), summary(updated, steady), false, report.OutputWide)

	require.Equal(t,
		"INF ℹ️ Updated at=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"INF ℹ️ Up to date at=app.txt:3 version=1.5.0\n"+
			"INF ℹ️ Run complete changed=1 skipped=0 failed=0\n",
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
		"ERR ❌ Invalid at=a.txt:1 error=boom\n"+
			"INF ℹ️ OK at=b.txt:2\n"+
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
		"INF ℹ️ Reformatted at=d/app.txt:1\n"+
			"DRY 🚧 Format complete changed=1\n",
		buf.String(),
	)
}
