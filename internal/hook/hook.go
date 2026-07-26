// Package hook runs the user-supplied commands around a run: a pre-exec hook
// whose failure aborts the update, and a post-exec hook told whether anything
// changed. The hook contract travels in environment variables, always all set,
// so a stale inherited value can never leak a false fact into a hook.
package hook

import (
	"cmp"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	xos "github.com/gechr/x/os"
)

// Environment variable names of the hook contract.
const (
	// PhaseEnv tells the hook which side of the run it is on: pre or post.
	PhaseEnv = "CLOVER_PHASE"
	// ChangedEnv tells the hook whether the run rewrote anything: unknown in
	// pre (the run has not happened), true or false in post.
	ChangedEnv = "CLOVER_CHANGED"
	// SuccessEnv tells the hook whether the run finished cleanly: unknown in
	// pre, false in post when any marker failed or a write errored.
	SuccessEnv = "CLOVER_SUCCESS"
)

// Phase values for PhaseEnv.
const (
	PhasePre  = "pre"
	PhasePost = "post"
)

// unknown is the tri-state value for facts the pre phase cannot know yet.
const unknown = "unknown"

// runner executes a prepared hook command; a test seam so unit tests never
// fork. The default forks the real process.
var runner = func(cmd *exec.Cmd) error { return cmd.Run() }

// Pre runs the pre-exec hook. The run outcome is unknown at this point, so the
// contract carries unknown for both facts.
func Pre(ctx context.Context, shell, command string) error {
	return run(ctx, shell, command, PhasePre, unknown, unknown)
}

// Post runs the post-exec hook, telling it whether the run changed anything and
// whether it finished without failures.
func Post(ctx context.Context, shell, command string, changed, success bool) error {
	return run(ctx, shell, command, PhasePost,
		strconv.FormatBool(changed), strconv.FormatBool(success))
}

// DefaultShell returns the platform's default hook shell, which the --exec-shell
// flag advertises and falls back to.
func DefaultShell() string {
	if xos.IsWindows() {
		return "cmd.exe"
	}
	return "/bin/sh"
}

// run executes the command through the shell, inheriting the working directory,
// environment, and stdio, with the contract variables appended. An empty shell
// selects the platform default; a shell is invoked as `<shell> -c <command>`,
// the convention sh, bash, zsh, fish, and pwsh all accept, except cmd, which
// takes /C.
func run(ctx context.Context, shell, command, phase, changed, success string) error {
	shell = cmp.Or(shell, DefaultShell())
	flag := "-c"
	if isCmdShell(shell) {
		flag = "/C"
	}
	cmd := exec.CommandContext(ctx, shell, flag, command)
	cmd.Env = append(os.Environ(),
		formatEnvVar(PhaseEnv, phase),
		formatEnvVar(ChangedEnv, changed),
		formatEnvVar(SuccessEnv, success),
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return runner(cmd)
}

// formatEnvVar renders one KEY=value environment entry.
func formatEnvVar(key, value string) string { return key + "=" + value }

// isCmdShell reports whether the shell is Windows cmd, the one shell that takes
// its command via /C rather than -c.
func isCmdShell(shell string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(shell)), ".exe")
	return base == "cmd"
}
