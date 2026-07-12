// Package progress is the engine-facing seam for reporting the live state of a
// batch of concurrent work - one task per unit (a marker resolving). The engine
// emits events through a Reporter; the rendering implementation (a clog live
// display on a TTY, plain lines otherwise) lives at the CLI edge, so the
// pipeline and its pure core never depend on a terminal.
package progress

// Reporter renders the progress of a batch of work whose size is known up front.
type Reporter interface {
	// Track starts a transient progress line labeled label, reporting the running
	// count through the named field. A total of zero shows an open counter, for
	// work whose size is unknown up front (a directory walk); a positive total
	// shows a fraction (N/total) with the same gradient the resolve line uses.
	// Stop ends the line, and the next log line supplants it.
	Track(label, field string, total int) Tracker
	// Discovered reports the scan totals - how many files were scanned, how many
	// carried directives, and how many directives in all - before resolution
	// begins.
	Discovered(scanned, files, comments int)
	// Begin registers one task per name, in order, and starts rendering. It
	// returns the task handles aligned to names and a wait function to call once
	// every task has reached a terminal state (Done, Fail, or Skip), which blocks
	// until rendering has drained.
	Begin(names []string) (tasks []Task, wait func())
}

// Tracker is a handle to a transient progress line. Set reports the running
// count as the work proceeds, from any goroutine; Stop ends the line.
type Tracker interface {
	// Set records the running count so far.
	Set(n int)
	// Stop ends the progress line; the next log line takes its place.
	Stop()
}

// Task is a handle to one unit of work. Any number of Update calls may precede a
// single terminal call - Done, Fail, or Skip.
type Task interface {
	// Update sets the live message shown while the task runs.
	Update(msg string)
	// Done marks the task succeeded, with a final message.
	Done(msg string)
	// Fail marks the task failed, with a reason.
	Fail(msg string)
	// Skip marks the task never ran because a dependency did not resolve.
	Skip(msg string)
}

// Nop is a Reporter that renders nothing - the default when no display is
// attached (off the engine's hot path, in tests, or for library use).
type Nop struct{}

// Track returns an inert tracker.
func (Nop) Track(string, string, int) Tracker { return nopTracker{} }

// Discovered reports nothing.
func (Nop) Discovered(int, int, int) {}

// Begin returns inert task handles and a no-op wait.
func (Nop) Begin(names []string) ([]Task, func()) {
	tasks := make([]Task, len(names))
	for i := range tasks {
		tasks[i] = nopTask{}
	}
	return tasks, func() {}
}

type nopTracker struct{}

func (nopTracker) Set(int) {}
func (nopTracker) Stop()   {}

type nopTask struct{}

func (nopTask) Update(string) {}
func (nopTask) Done(string)   {}
func (nopTask) Fail(string)   {}
func (nopTask) Skip(string)   {}
