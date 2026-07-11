package command_test

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gechr/clive/updater"
	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// captureStdout swaps os.Stdout for a pipe, runs f, and returns what it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	f()
	require.NoError(t, w.Close())
	os.Stdout = orig
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// captureStderr swaps os.Stderr for a pipe, runs f, and returns what it printed.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	f()
	require.NoError(t, w.Close())
	os.Stderr = orig
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

func TestRunVersion(t *testing.T) {
	t.Parallel()

	require.NoError(t, command.RunVersion(false), "the concise version prints without error")
	require.NoError(t, command.RunVersion(true), "the detailed build info prints without error")
}

func TestUpdateConfig(t *testing.T) {
	t.Parallel()

	// updateConfig is a pure construction of the self-update config, so it builds
	// without panicking.
	require.NotPanics(t, func() { _ = command.UpdateConfig() })
}

func TestReportExit(t *testing.T) {
	t.Parallel()

	cases := map[string]error{
		"reported is silent":     updater.ErrReported,
		"wrapped reported":       fmt.Errorf("self-update: %w", updater.ErrReported),
		"failures summary":       command.FailuresError(3),
		"wrapped failures":       fmt.Errorf("run: %w", command.FailuresError(1)),
		"plain error is generic": os.ErrClosed,
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NotPanics(t, func() { command.ReportExit(err) })
		})
	}
}

func TestLaunch(t *testing.T) {
	t.Parallel()

	// The version-omitted branch runs in the test binary (clive.Current() empty).
	require.NotPanics(t, func() { command.Launch() })
}

// TestCompletionHandler exercises the dynamic completion dispatch: a provider
// predictor prints the registered provider, a tag predictor and an unknown kind
// print nothing extra here (no directives in this dir), and none panic.
func TestCompletionHandler(t *testing.T) {
	provider.Register(authedProvider{name: "complhandler"})

	out := captureStdout(t, func() {
		command.CompletionHandler("", command.PredictorProvider, nil)
	})
	require.Contains(t, splitLines(out), "complhandler", "the provider predictor lists providers")

	require.NotPanics(t, func() {
		command.CompletionHandler("", "unknown-kind", nil)
	}, "an unrecognized predictor is a no-op")
}

func TestCompleteProviders(t *testing.T) {
	provider.Register(authedProvider{name: "complprov"})

	out := captureStdout(t, command.CompleteProviders)
	require.Contains(t, splitLines(out), "complprov",
		"every selectable provider is offered on its own line")
}

func TestCompleteTags(t *testing.T) {
	t.Run("prints sorted unique tags", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			dir+"/Dockerfile",
			[]byte(
				"# clover: provider=docker repository=nginx tags=foo,bar,foo\nFROM nginx:1.27\n",
			),
			0o644,
		))
		t.Chdir(dir)

		out := captureStdout(t, command.CompleteTags)
		require.Equal(t, "bar\nfoo\n", out, "tags are unique and naturally sorted")
	})

	t.Run("empty tree prints nothing", func(t *testing.T) {
		t.Chdir(t.TempDir())
		require.Empty(t, captureStdout(t, command.CompleteTags))
	})
}

func TestRunInitRequiresTerminal(t *testing.T) {
	t.Parallel()

	// The test binary has no TTY, so init fails its interactive guard before the
	// wizard.
	require.EqualError(t, command.RunInit(t.TempDir()), "init needs an interactive terminal")
}

// splitLines splits captured output into its non-empty lines for slice-membership
// assertions.
func splitLines(s string) []string {
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
