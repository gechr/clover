package logger_test

import (
	"testing"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/logger"
	"github.com/gechr/conductor"
	"github.com/stretchr/testify/require"
)

func TestSetVerboseEnablesDebugLogs(t *testing.T) {
	t.Setenv("CLOG_LOG_LEVEL", "")
	clog.SetLevel(clog.LevelInfo)
	t.Cleanup(func() { clog.SetLevel(clog.LevelInfo) })

	logger.SetVerbose(true)

	require.True(t, clog.IsVerbose())
	require.Equal(t, clog.LevelDebug, clog.GetLevel())
}

func TestConductorInitLoadsCloverHyperlinkFormat(t *testing.T) {
	t.Setenv("CLOVER_HYPERLINK_FORMAT", "vscode")
	t.Setenv("CLOG_HYPERLINK_FORMAT", "")
	t.Cleanup(func() {
		clog.SetEnvPrefix(clog.DefaultEnvPrefix)
		clog.SetFieldFormats(clog.DefaultFieldFormats())
	})

	// The env prefix now comes from conductor (derived from the app name),
	// with Clover's own customisations layered on via ConfigureLog.
	conductor.New(conductor.App{Name: "clover", ConfigureLog: logger.Configure})

	formats := clog.Default.FieldFormats()
	require.Equal(t, "vscode://file{path}", formats.HyperlinkPathFormat)
	require.Equal(t, "vscode://file{path}:{line}", formats.HyperlinkLineFormat)
	require.Equal(t, "vscode://file{path}:{line}:{column}", formats.HyperlinkColumnFormat)
}
