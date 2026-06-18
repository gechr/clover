package exec

import (
	"cmp"
	"context"
	"fmt"
	"sync"
)

// Task is a node in the follow-edge dependency graph: an optional ID others may
// follow, the ID it follows (From, empty when independent), and the work to run.
// Run is provider-agnostic - the pipeline supplies a closure that resolves a
// producer or a follower - so the scheduler stays a pure graph executor.
//
// ID and From may be opaque, namespaced keys; Label and FromLabel are their
// human-readable forms used in skip reasons so an internal key never reaches the
// user. Each defaults to its key when unset.
type Task struct {
	ID        string
	From      string
	Label     string
	FromLabel string
	Run       func(ctx context.Context) error
}

// label returns the display name for the task's ID, falling back to the ID.
func (t Task) label() string { return cmp.Or(t.Label, t.ID) }

// fromLabel returns the display name for the task's From, falling back to From.
func (t Task) fromLabel() string { return cmp.Or(t.FromLabel, t.From) }

// Result is the outcome of a task. Exactly one of Err being set, Skipped being
// true, or a clean success holds. Skipped means the task never ran because its
// dependency failed, was missing, or formed a cycle.
type Result struct {
	ID      string
	Err     error
	Skipped bool
	Reason  string
}

// Execute runs tasks honouring their from-edges: a task runs only after the task
// it follows has succeeded. Independent tasks run first, up to workers at a
// time; followers run in later waves. A task whose dependency fails, is unknown,
// or lies on a cycle is skipped rather than aborting the run, so one bad marker
// never sinks the rest. Results are returned in input order.
func Execute(ctx context.Context, tasks []Task, workers int) []Result {
	results := make([]Result, len(tasks))
	for i := range tasks {
		results[i] = Result{ID: tasks[i].ID}
	}
	if len(tasks) == 0 {
		return results
	}
	if workers < 1 {
		workers = 1
	}

	parent, reason := analyze(tasks)
	for i := range tasks {
		if reason[i] != "" {
			results[i] = Result{ID: tasks[i].ID, Skipped: true, Reason: reason[i]}
		}
	}

	for _, wave := range waves(parent, reason) {
		runnable := wave[:0:0]
		for _, i := range wave {
			if p := parent[i]; p >= 0 && !succeeded(results[p]) {
				results[i] = Result{
					ID:      tasks[i].ID,
					Skipped: true,
					Reason:  fmt.Sprintf("dependency %q did not resolve", tasks[p].label()),
				}
				continue
			}
			runnable = append(runnable, i)
		}
		runWave(ctx, runnable, tasks, results, workers)
	}
	return results
}

// succeeded reports whether a task ran cleanly.
func succeeded(r Result) bool { return !r.Skipped && r.Err == nil }

// analyze resolves each task's parent index (or -1) and the reason it cannot be
// scheduled: a from that names no task, or a from-chain that cycles or descends
// from such a task.
func analyze(tasks []Task) ([]int, []string) {
	parent := make([]int, len(tasks))
	reason := make([]string, len(tasks))

	byID := make(map[string]int, len(tasks))
	for i, t := range tasks {
		if t.ID != "" {
			byID[t.ID] = i
		}
	}
	for i, t := range tasks {
		parent[i] = -1
		if t.From == "" {
			continue
		}
		if j, ok := byID[t.From]; ok {
			parent[i] = j
		} else {
			reason[i] = fmt.Sprintf("unknown from %q", t.fromLabel())
		}
	}

	const (
		unvisited = iota
		visiting
		resolvable
		unresolvable
	)
	state := make([]int, len(tasks))
	var resolve func(i int) bool
	resolve = func(i int) bool {
		switch state[i] {
		case resolvable:
			return true
		case unresolvable, visiting: // visiting again means a cycle
			return false
		}
		state[i] = visiting

		ok := reason[i] == "" && (parent[i] < 0 || resolve(parent[i]))
		if ok {
			state[i] = resolvable
		} else {
			state[i] = unresolvable
		}
		return ok
	}
	for i := range tasks {
		if !resolve(i) && reason[i] == "" {
			reason[i] = "from-chain cycles or descends from an unknown task"
		}
	}
	return parent, reason
}

// waves groups the schedulable tasks by depth in the from-chain, so wave n
// depends only on waves before it. Unschedulable tasks (reason set) are omitted.
func waves(parent []int, reason []string) [][]int {
	var depth func(i int) int
	depth = func(i int) int {
		if parent[i] < 0 {
			return 0
		}
		return depth(parent[i]) + 1
	}

	var out [][]int
	for i := range parent {
		if reason[i] != "" {
			continue
		}
		d := depth(i)
		for len(out) <= d {
			out = append(out, nil)
		}
		out[d] = append(out[d], i)
	}
	return out
}

// runWave runs the given tasks concurrently, capped at workers. Each result is
// written to its own index, so the shared slice needs no lock.
func runWave(ctx context.Context, runnable []int, tasks []Task, results []Result, workers int) {
	slots := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, i := range runnable {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			results[i].Err = tasks[i].Run(ctx)
		}()
	}
	wg.Wait()
}
