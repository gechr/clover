package hook

import "os/exec"

// SetRunner swaps the executor seam and returns a restore func, so unit tests
// can capture the prepared command without forking a process.
func SetRunner(fn func(*exec.Cmd) error) func() {
	prev := runner
	runner = fn
	return func() { runner = prev }
}
