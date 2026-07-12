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
	return pipeline.Result{Marker: pipeline.Marker{File: file, Target: target, Provider: "github"}}
}

func TestRun(t *testing.T) {
	updated := result("x/app.txt", 1)
	updated.Current, updated.Resolved, updated.Changed = "1.2.0", "1.3.0", true
	skipped := result("x/b.txt", 3)
	skipped.Skipped, skipped.Reason = true, "dep failed"

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(updated, skipped), false, output.Text)

	require.Equal(t,
		"INF ⬆️ Update applied provider=github location=x/app.txt:2 from=1.2.0 to=1.3.0\n"+
			"WRN 📛 Skipped provider=github location=x/b.txt:4 reason=\"dep failed\"\n"+
			"INF 🏁 Run complete changed=1 skipped=1 elapsed=2s\n",
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
		"INF ⬆️ Update applied provider=github location=app.txt:1 from=1.20.0 to=1.31.2\n"+
			"INF ⬆️ Update applied provider=github location=app.txt:3 from=1.0.0 to=2.0.0\n"+
			"INF 🏁 Run complete changed=2 elapsed=2s\n",
		buf.String(),
	)
}

func TestAnnotatePreview(t *testing.T) {
	s := mode.AnnotateSummary{Files: []mode.AnnotateFile{{
		Path: "Dockerfile",
		Changes: []mode.AnnotateChange{
			{At: 0, Provider: "docker"},
			{At: 4, Provider: "docker", Existing: true},
		},
	}}, Elapsed: 2 * time.Second}

	var buf bytes.Buffer
	report.Annotate(clog.NewWriter(&buf), s, false)

	require.Equal(t,
		"DRY 🚧 Would annotate provider=docker location=Dockerfile:1\n"+
			"DRY 🚧 Would reannotate provider=docker location=Dockerfile:5\n"+
			"DRY 🏁 Annotate complete added=1 updated=1 elapsed=2s\n",
		buf.String(),
	)
}

func TestAnnotateWrites(t *testing.T) {
	s := mode.AnnotateSummary{Files: []mode.AnnotateFile{{
		Path:    "Dockerfile",
		Written: true,
		Changes: []mode.AnnotateChange{{At: 0, Provider: "docker"}},
	}}, Elapsed: 2 * time.Second}

	var buf bytes.Buffer
	report.Annotate(clog.NewWriter(&buf), s, true)

	require.Equal(t,
		"INF 🌱 Annotated provider=docker location=Dockerfile:1\n"+
			"INF 🏁 Annotate complete added=1 elapsed=2s\n",
		buf.String(),
	)
}

func TestAnnotateLogsResource(t *testing.T) {
	s := mode.AnnotateSummary{Files: []mode.AnnotateFile{{
		Path: ".github/workflows/ci.yaml",
		Changes: []mode.AnnotateChange{{
			At:          20,
			Provider:    "github",
			Resource:    "actions/checkout",
			ResourceURL: "https://github.com/actions/checkout",
		}},
	}}, Elapsed: 2 * time.Second}

	var buf bytes.Buffer
	report.Annotate(clog.NewWriter(&buf), s, false)

	require.Equal(
		t,
		"DRY 🚧 Would annotate provider=github location=.github/workflows/ci.yaml:21 resource=actions/checkout\n"+
			"DRY 🏁 Annotate complete added=1 elapsed=2s\n",
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
	blocked := result("w.yml", 3)
	blocked.Current, blocked.Resolved = "1.0.0", "2.0.0"
	blocked.Verify = errors.New("commit abc is not on an allowed branch")
	moved := result("d.txt", 5)
	moved.Current, moved.Moved = "aaa", "bbb"

	var buf bytes.Buffer
	report.GitHub(&buf, summary(updated, failed, skipped, pinned, blocked, moved), true)

	require.Equal(t,
		"::warning file=x/app.txt,line=2::update available: 1.2.0 → 1.3.0\n"+
			"::error file=y/b.txt,line=1::boom\n"+
			"::warning file=c.txt,line=5::skipped: dep failed\n"+
			"::error file=ci.yml,line=3::pin does not match upstream: commit abc is not on an allowed branch\n"+
			"::error file=w.yml,line=4::update blocked: commit abc is not on an allowed branch\n"+
			"::warning file=d.txt,line=6::pinned upstream tag has moved: aaa → bbb\n",
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

// TestRunReportsBlockedUpdate confirms a resolved update that failed pin
// verification is reported with the update it withheld.
func TestRunReportsBlockedUpdate(t *testing.T) {
	blocked := result("ci.yml", 0)
	blocked.Current, blocked.Resolved = "1.0.0", "2.0.0"
	blocked.Verify = errors.New("commit abc is not on an allowed branch")

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(blocked), false, output.Text)

	require.Equal(t,
		"ERR 🚫 Update blocked provider=github location=ci.yml:1 from=1.0.0 to=2.0.0 "+
			"error=\"commit abc is not on an allowed branch\"\n"+
			"INF 🏁 Run complete elapsed=2s\n",
		buf.String(),
	)
}

// TestRunReportsPinVerification confirms an up-to-date pin that no longer
// matches upstream is reported as the marker's outcome.
func TestRunReportsPinVerification(t *testing.T) {
	upToDate := result("ci.yml", 0)
	upToDate.Verify = errors.New("pinned aaa but 1.0.0 upstream is bbb")

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(upToDate), false, output.Text)

	require.Equal(t,
		"ERR 🔓 Pin does not match upstream provider=github location=ci.yml:1 "+
			"error=\"pinned aaa but 1.0.0 upstream is bbb\"\n"+
			"INF 🏁 Run complete elapsed=2s\n",
		buf.String(),
	)
}

// TestRunReportsMovedTag confirms a held pin whose upstream tag moved is warned
// alongside its outcome, with the held and moved-to commits abbreviated.
func TestRunReportsMovedTag(t *testing.T) {
	const (
		oldSHA = "0123456789abcdef0123456789abcdef01234567"
		newSHA = "fedcba9876543210fedcba9876543210fedcba98"
	)
	held := result("ci.yml", 0)
	held.Current, held.Moved = oldSHA, newSHA

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(held), false, output.Text)

	require.Equal(t,
		"WRN 🔀 Pinned upstream tag has moved (pass `--force` to re-pin if safe) "+
			"provider=github location=ci.yml:1 from=012345…234567 to=fedcba…dcba98\n"+
			"INF 🏁 Run complete elapsed=2s\n",
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

	require.Equal(
		t,
		"INF ⬆️ Update applied provider=github location=ci.yml:1 from=012345…234567 to=fedcba…dcba98\n"+
			"INF 🏁 Run complete changed=1 elapsed=2s\n",
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
		"INF ⬆️ Update applied provider=github location=ci.yml:1 from=1.0.0 to="+sha+"\n"+
			"INF 🏁 Run complete changed=1 elapsed=2s\n",
		buf.String(),
	)
}

func TestRunDryLogsSummaryAtDryLevel(t *testing.T) {
	updated := result("app.txt", 0)
	updated.Current, updated.Resolved, updated.Changed = "1.0.0", "2.0.0", true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(updated), true, output.Text)

	require.Equal(t,
		"DRY ⬆️ Update available provider=github location=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"DRY 🏁 Run complete changed=1 elapsed=2s\n",
		buf.String(),
	)
}

// TestRunLogsResource confirms a change carries the resolved provider and, when
// the provider names one, a resource= field; a result without a resource omits
// it entirely.
func TestRunLogsResource(t *testing.T) {
	named := result("ci.yml", 0)
	named.Current, named.Resolved, named.Changed = "1.0.0", "2.0.0", true
	named.Resource, named.ResourceURL = "actions/checkout", "https://github.com/actions/checkout"
	bare := result("ci.yml", 2)
	bare.Current, bare.Resolved, bare.Changed = "3.0.0", "4.0.0", true

	var buf bytes.Buffer
	report.Run(clog.NewWriter(&buf), summary(named, bare), false, output.Text)

	require.Equal(
		t,
		"INF ⬆️ Update applied provider=github location=ci.yml:1 resource=actions/checkout from=1.0.0 to=2.0.0\n"+
			"INF ⬆️ Update applied provider=github location=ci.yml:3 from=3.0.0 to=4.0.0\n"+
			"INF 🏁 Run complete changed=2 elapsed=2s\n",
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
		"ERR ❌ Invalid provider=github location=a.txt:1 error=boom\n"+
			"INF 🏁 Lint complete errored=1 elapsed=2s\n",
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
		"INF ⬆️ Update applied provider=github location=app.txt:1 from=1.0.0 to=2.0.0\n"+
			"DBG 🐞 Already up-to-date provider=github location=app.txt:3 version=1.5.0\n"+
			"INF 🏁 Run complete changed=1 elapsed=2s\n",
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
		"ERR ❌ Invalid provider=github location=a.txt:1 error=boom\n"+
			"INF ✅ Valid provider=github location=b.txt:2\n"+
			"INF 🏁 Lint complete errored=1 elapsed=2s\n",
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
