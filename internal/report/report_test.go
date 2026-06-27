package report_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/output"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/report"
	"github.com/stretchr/testify/require"
)

// summary wraps results into a one-file mode.Summary with a fixed elapsed time,
// so the run summary line renders deterministically.
func summary(results ...pipeline.Result) mode.Summary {
	return mode.Summary{
		Outcomes: []mode.Outcome{{FileResult: pipeline.FileResult{Results: results}}},
		Elapsed:  1500 * time.Millisecond,
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
	report.Run(clog.NewWriter(&buf), summary(updated, skipped), false, output.Text)

	require.Equal(t,
		"INF ⬆️ Update applied location=x/app.txt:2 from=1.2.0 to=1.3.0\n"+
			"WRN ⚠️ Skipped location=x/b.txt:4 reason=\"dep failed\"\n"+
			"INF 🏁 Run complete changed=1 skipped=1 failed=0 elapsed=1.5s\n",
		buf.String(),
	)
}

// TestRunReportsWrittenValue confirms a change reports the value actually written
// to the line (Written) - here a variant stripped to plain - not the raw resolved
// candidate, so the preview matches the file. It falls back to Resolved when no
// Written value was recorded (a follower projecting its value verbatim).
func TestRunReportsWrittenValue(t *testing.T) {
	written := result("app.txt", 0)
	written.Current, written.Resolved, written.Written, written.Changed = "1.20.0", "1.31.2-alpine", "1.31.2", true
	fallback := result("app.txt", 2)
	fallback.Current, fallback.Resolved, fallback.Changed = "1.0.0", "2.0.0", true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(written, fallback), false, output.Text)

	require.Equal(t,
		"INF ⬆️ Update applied location=app.txt:1 from=1.20.0 to=1.31.2\n"+
			"INF ⬆️ Update applied location=app.txt:3 from=1.0.0 to=2.0.0\n"+
			"INF 🏁 Run complete changed=2 skipped=0 failed=0 elapsed=1.5s\n",
		buf.String(),
	)
}

func TestGitHub(t *testing.T) {
	updated := result("x/app.txt", 1) // target 1 -> line 2
	updated.Current, updated.Resolved, updated.Changed = "1.2.0", "1.3.0", true
	failed := result("y/b.txt", 0)
	failed.Err = errors.New("boom")
	skipped := result("c.txt", 4)
	skipped.Skipped, skipped.Reason = true, "dep failed"
	pinned := result("ci.yml", 2)
	pinned.Verify = errors.New("commit abc is not on an allowed branch")

	var buf bytes.Buffer
	report.GitHub(&buf, summary(updated, failed, skipped, pinned), true)

	require.Equal(t,
		"::warning file=x/app.txt,line=2::update available: 1.2.0 → 1.3.0\n"+
			"::error file=y/b.txt,line=1::boom\n"+
			"::warning file=c.txt,line=5::skipped: dep failed\n"+
			"::error file=ci.yml,line=3::pin does not match upstream: commit abc is not on an allowed branch\n",
		buf.String(),
	)
}

func TestGitHubEscapes(t *testing.T) {
	// A path with a colon and a message with a newline are escaped per the
	// workflow-command grammar.
	r := result("a:b.txt", 0)
	r.Err = errors.New("oops\nagain")

	var buf bytes.Buffer
	report.GitHub(&buf, summary(r), false)

	require.Equal(t, "::error file=a%3Ab.txt,line=1::oops%0Aagain\n", buf.String())
}

// TestRunReportsPinVerification confirms a non-fatal pin mismatch is logged
// alongside the marker's outcome, not in place of it.
func TestRunReportsPinVerification(t *testing.T) {
	upToDate := result("ci.yml", 0)
	upToDate.Verify = errors.New("pinned aaa but 1.0.0 upstream is bbb")

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(upToDate), false, output.Text)

	require.Equal(t,
		"ERR 🔓 Pin does not match upstream location=ci.yml:1 "+
			"error=\"pinned aaa but 1.0.0 upstream is bbb\"\n"+
			"INF 🏁 Run complete changed=0 skipped=0 failed=0 elapsed=1.5s\n",
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
	report.Run(clog.NewWriter(&buf), summary(pinned), false, output.Text)

	require.Equal(t,
		"INF ⬆️ Update applied location=ci.yml:1 from=012345…234567 to=fedcba…dcba98\n"+
			"INF 🏁 Run complete changed=1 skipped=0 failed=0 elapsed=1.5s\n",
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
	report.Run(clog.NewWriter(&buf), summary(pinned), false, output.Wide)

	require.Equal(t,
		"INF ⬆️ Update applied location=ci.yml:1 from=1.0.0 to="+sha+"\n"+
			"INF 🏁 Run complete changed=1 skipped=0 failed=0 elapsed=1.5s\n",
		buf.String(),
	)
}

func TestRunDryLogsSummaryAtDryLevel(t *testing.T) {
	updated := result("app.txt", 0)
	updated.Current, updated.Resolved, updated.Changed = "1.0.0", "2.0.0", true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(updated), true, output.Text)

	require.Equal(t,
		"DRY ⬆️ Update available location=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"DRY 🏁 Run complete changed=1 skipped=0 failed=0 elapsed=1.5s\n",
		buf.String(),
	)
}

// TestRunEmptyLogsNoSummary confirms a run with no markers logs nothing - the
// "No Clover comments found" warning stands alone, with no "Run complete".
func TestRunEmptyLogsNoSummary(t *testing.T) {
	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), mode.Summary{}, false, output.Text)
	require.Empty(t, buf.String())
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
		"INF ✨ Formatted location=dir/app.txt:5\n"+
			"INF 🏁 Format complete changed=1\n",
		buf.String(),
	)
}

func TestLint(t *testing.T) {
	bad := result("a.txt", 0)
	bad.Err = errors.New("boom")

	var buf bytes.Buffer
	report.Lint(clog.NewWriter(&buf), summary(bad), output.Text)

	require.Equal(t,
		"ERR ❌ Invalid location=a.txt:1 error=boom\n"+
			"INF 🏁 Lint complete errored=1 skipped=0\n",
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
	report.Run(logger, summary(updated, steady), false, output.Wide)

	require.Equal(t,
		"INF ⬆️ Update applied location=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"DBG 🐞 Already up-to-date location=app.txt:3 version=1.5.0\n"+
			"INF 🏁 Run complete changed=1 skipped=0 failed=0 elapsed=1.5s\n",
		buf.String(),
	)
}

func TestLintWideReportsValid(t *testing.T) {
	bad := result("a.txt", 0)
	bad.Err = errors.New("boom")
	ok := result("b.txt", 1) // valid: no error, not skipped

	var buf bytes.Buffer
	report.Lint(clog.NewWriter(&buf), summary(bad, ok), output.Wide)

	require.Equal(t,
		"ERR ❌ Invalid location=a.txt:1 error=boom\n"+
			"INF ✅ Valid location=b.txt:2\n"+
			"INF 🏁 Lint complete errored=1 skipped=0\n",
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
		"DRY 🚧 Would format location=d/app.txt:1\n"+
			"DRY 🏁 Format complete changed=1\n",
		buf.String(),
	)
}
