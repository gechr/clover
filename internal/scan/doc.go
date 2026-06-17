// Package scan walks the requested paths and discovers the files that contain a
// cusp: directive. A single walker feeds a pool of workers that read and scan
// files concurrently; only files with a directive keep their content, for the
// later rewrite. Binary and oversized files are skipped, as are VCS and ignored
// directories. Discovery only - atomic rewrites belong to the apply phase
// (x/os.AtomicWrite).
package scan
