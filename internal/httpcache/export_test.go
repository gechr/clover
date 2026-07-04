package httpcache

// ResetSharedDisk clears the process-wide disk store, restoring the in-memory
// default for tests that enable it.
func ResetSharedDisk() { sharedDisk.Store(nil) }
