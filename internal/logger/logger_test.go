package logger_test

import (
	"testing"

	"github.com/gechr/clog"
	"github.com/gechr/clover/internal/logger"
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
