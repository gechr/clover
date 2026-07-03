package exec

import "sync"

// Parallel runs fn(0) through fn(n-1) concurrently, at most workers in flight,
// and blocks until all complete. Each call gets a distinct index, so a goroutine
// writing results[i] needs no lock; fn must otherwise be safe to call
// concurrently. workers < 1 runs one at a time.
//
// It is the bounded parallel-for the per-file modes (format, annotate) and the
// per-marker validation and the pipeline's task waves share.
func Parallel(workers, n int, fn func(i int)) {
	if workers < 1 {
		workers = 1
	}
	slots := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			fn(i)
		}()
	}
	wg.Wait()
}
