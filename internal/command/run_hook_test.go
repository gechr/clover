package command_test

import (
	"context"
	"errors"
	"testing"

	"github.com/gechr/clover/internal/command"
	"github.com/gechr/clover/internal/mode"
	"github.com/gechr/clover/internal/model"
	"github.com/gechr/clover/internal/pipeline"
	"github.com/gechr/clover/internal/provider"
	"github.com/stretchr/testify/require"
)

// hookCall records one hook invocation crossing the command seam.
type hookCall struct {
	phase, shell, command string
	changed, success      bool
}

// stubHooks swaps both hook seams for recorders, restoring them when the test
// ends, and returns the calls plus per-phase errors to inject.
func stubHooks(t *testing.T, preErr, postErr error) *[]hookCall {
	t.Helper()
	var calls []hookCall
	restore := command.SetHooks(
		func(_ context.Context, shell, cmd string) error {
			calls = append(calls, hookCall{phase: "pre", shell: shell, command: cmd})
			return preErr
		},
		func(_ context.Context, shell, cmd string, changed, success bool) error {
			calls = append(calls, hookCall{
				phase: "post", shell: shell, command: cmd, changed: changed, success: success,
			})
			return postErr
		},
	)
	t.Cleanup(restore)
	return &calls
}

// TestRunHooks drives the run command with stubbed hook seams, covering the
// abort, ordering, outcome-signaling, and dry-run rules.
func TestRunHooks(t *testing.T) {
	t.Setenv("CLOVER_NO_CACHE", "1")
	provider.Register(stubProvider{
		name:       "hookok",
		candidates: []model.Candidate{model.NewCandidate("1.5.0")},
	})
	body := "# clover: provider=hookok repository=x/y\nversion: 1.2.0\n"
	current := "# clover: provider=hookok repository=x/y\nversion: 1.5.0\n"
	run := func(dir string, dryRun bool, opts ...command.RunOption) error {
		return command.RunRun(
			[]string{dir},
			dryRun,
			false,
			nil,
			nil,
			nil,
			"",
			nil,
			resolver(),
			4,
			opts...)
	}

	t.Run("failing pre-exec aborts before any write", func(t *testing.T) {
		calls := stubHooks(t, errors.New("boom"), nil)
		dir, path := writeMarker(t, body)
		err := run(dir, false, command.WithPreExec("guard"), command.WithPostExec("after"))
		require.EqualError(t, err, "pre-exec hook: boom")
		require.Equal(t, body, readAt(t, path), "an aborted run writes nothing")
		require.Equal(t, []hookCall{{phase: "pre", command: "guard"}}, *calls,
			"the post-exec hook does not run after an abort")
	})

	t.Run("post-exec sees a changed clean run", func(t *testing.T) {
		calls := stubHooks(t, nil, nil)
		dir, path := writeMarker(t, body)
		require.NoError(t, run(
			dir,
			false,
			command.WithPreExec(
				"guard",
			),
			command.WithPostExec("after"),
			command.WithExecShell("fish"),
		))
		require.Equal(t, current, readAt(t, path))
		require.Equal(t, []hookCall{
			{phase: "pre", shell: "fish", command: "guard"},
			{phase: "post", shell: "fish", command: "after", changed: true, success: true},
		}, *calls)
	})

	t.Run("post-exec sees an unchanged run", func(t *testing.T) {
		calls := stubHooks(t, nil, nil)
		dir, _ := writeMarker(t, current)
		require.NoError(t, run(dir, false, command.WithPostExec("after")))
		require.Equal(t, []hookCall{
			{phase: "post", command: "after", changed: false, success: true},
		}, *calls)
	})

	t.Run("post-exec still runs after marker failures", func(t *testing.T) {
		calls := stubHooks(t, nil, nil)
		dir, _ := writeMarker(t, "# clover: provider=hookghost repository=x/y\nversion: 1.0.0\n")
		err := run(dir, false, command.WithPostExec("after"))
		require.EqualError(t, err, "1 failed")
		require.Equal(t, []hookCall{
			{phase: "post", command: "after", changed: false, success: false},
		}, *calls)
	})

	t.Run("post-exec failure fails a clean run", func(t *testing.T) {
		stubHooks(t, nil, errors.New("boom"))
		dir, _ := writeMarker(t, body)
		err := run(dir, false, command.WithPostExec("after"))
		require.EqualError(t, err, "post-exec hook: boom")
	})

	t.Run("marker failures are not masked by a post-exec failure", func(t *testing.T) {
		stubHooks(t, nil, errors.New("boom"))
		dir, _ := writeMarker(t, "# clover: provider=hookghost repository=x/y\nversion: 1.0.0\n")
		err := run(dir, false, command.WithPostExec("after"))
		require.EqualError(t, err, "1 failed")
	})

	t.Run("post-exec sees a failed write as unchanged", func(t *testing.T) {
		calls := stubHooks(t, nil, nil)
		summary := mode.Summary{Outcomes: []mode.Outcome{{
			Results:  []pipeline.Result{{Changed: true}},
			Written:  false,
			WriteErr: errors.New("read-only"),
		}}}
		err := command.RunFinish("after", summary)
		require.EqualError(t, err, "1 file could not be written")
		require.Equal(t, []hookCall{
			{phase: "post", command: "after", changed: false, success: false},
		}, *calls, "a rendered update that never landed is not a change")
	})

	t.Run("post-exec sees a partial write as changed but failed", func(t *testing.T) {
		calls := stubHooks(t, nil, nil)
		summary := mode.Summary{Outcomes: []mode.Outcome{
			{
				Results: []pipeline.Result{{Changed: true}},
				Written: true,
			},
			{
				Results:  []pipeline.Result{{Changed: true}},
				WriteErr: errors.New("read-only"),
			},
		}}
		err := command.RunFinish("after", summary)
		require.EqualError(t, err, "1 file could not be written")
		require.Equal(t, []hookCall{
			{phase: "post", command: "after", changed: true, success: false},
		}, *calls)
	})

	t.Run("dry run skips both hooks", func(t *testing.T) {
		calls := stubHooks(t, nil, nil)
		dir, path := writeMarker(t, body)
		require.NoError(t, run(dir, true,
			command.WithPreExec("guard"), command.WithPostExec("after")))
		require.Equal(t, body, readAt(t, path))
		require.Empty(t, *calls, "--dry-run previews without side effects")
	})
}
