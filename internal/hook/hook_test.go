package hook_test

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/gechr/clover/internal/hook"
	xos "github.com/gechr/x/os"
	"github.com/stretchr/testify/require"
)

// capture swaps the executor seam for one that records the prepared command
// without forking, restoring it when the test ends.
func capture(t *testing.T, err error) *[]*exec.Cmd {
	t.Helper()
	var cmds []*exec.Cmd
	restore := hook.SetRunner(func(cmd *exec.Cmd) error {
		cmds = append(cmds, cmd)
		return err
	})
	t.Cleanup(restore)
	return &cmds
}

// contractEnv returns the CLOVER_* contract entries from the prepared command's
// environment, keyed by variable name.
func contractEnv(t *testing.T, cmd *exec.Cmd) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, name := range []string{hook.PhaseEnv, hook.ChangedEnv, hook.SuccessEnv} {
		for _, entry := range slices.Backward(cmd.Env) {
			if after, ok := strings.CutPrefix(entry, name+"="); ok {
				got[name] = after
				break
			}
		}
	}
	return got
}

// TestPre confirms the pre phase carries unknown for both facts, since the run
// has not happened yet.
func TestPre(t *testing.T) {
	cmds := capture(t, nil)

	require.NoError(t, hook.Pre(context.Background(), "", "echo hi"))
	require.Len(t, *cmds, 1)
	cmd := (*cmds)[0]

	require.Equal(t, map[string]string{
		hook.PhaseEnv:   "pre",
		hook.ChangedEnv: "unknown",
		hook.SuccessEnv: "unknown",
	}, contractEnv(t, cmd))
	require.Equal(t, "echo hi", cmd.Args[len(cmd.Args)-1])
}

// TestPost confirms the post phase renders the run outcome as booleans.
func TestPost(t *testing.T) {
	tests := map[string]struct {
		changed, success bool
		want             map[string]string
	}{
		"changed clean": {changed: true, success: true, want: map[string]string{
			hook.PhaseEnv: "post", hook.ChangedEnv: "true", hook.SuccessEnv: "true",
		}},
		"unchanged failed": {changed: false, success: false, want: map[string]string{
			hook.PhaseEnv: "post", hook.ChangedEnv: "false", hook.SuccessEnv: "false",
		}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cmds := capture(t, nil)
			require.NoError(
				t,
				hook.Post(context.Background(), "", "echo hi", tc.changed, tc.success),
			)
			require.Len(t, *cmds, 1)
			require.Equal(t, tc.want, contractEnv(t, (*cmds)[0]))
		})
	}
}

// TestShell confirms the platform default and the --exec-shell override.
func TestShell(t *testing.T) {
	t.Run("default is the platform shell", func(t *testing.T) {
		cmds := capture(t, nil)
		require.NoError(t, hook.Pre(context.Background(), "", "true"))
		want := []string{"/bin/sh", "-c", "true"}
		if xos.IsWindows() {
			want = []string{"cmd.exe", "/C", "true"}
		}
		require.Equal(t, want, (*cmds)[0].Args)
	})

	t.Run("override is invoked as shell -c", func(t *testing.T) {
		cmds := capture(t, nil)
		require.NoError(t, hook.Pre(context.Background(), "fish", "true"))
		require.Equal(t, []string{"fish", "-c", "true"}, (*cmds)[0].Args)
	})

	t.Run("cmd takes /C on any platform", func(t *testing.T) {
		cmds := capture(t, nil)
		require.NoError(t, hook.Pre(context.Background(), "CMD.EXE", "true"))
		require.Equal(t, "/C", (*cmds)[0].Args[1])
	})
}

// TestError confirms a hook failure propagates to the caller.
func TestError(t *testing.T) {
	errBoom := errors.New("boom")
	capture(t, errBoom)
	require.Equal(t, errBoom, hook.Post(context.Background(), "", "true", true, true))
}

// TestFork forks a real shell once, confirming exit statuses cross the seam:
// `exit 0` and `exit 1` are the two commands sh and cmd.exe agree on.
func TestFork(t *testing.T) {
	require.NoError(t, hook.Pre(context.Background(), "", "exit 0"))
	require.Error(t, hook.Pre(context.Background(), "", "exit 1"))
}
