package mode

import (
	"fmt"
	"os"
	"strings"

	xos "github.com/gechr/x/os"
)

// writeSidecar atomically writes a generated sidecar's content.
func writeSidecar(s *AnnotateSidecar) error {
	return writeNew(s.Path, []byte(s.Content))
}

// defaultFileMode is the mode a freshly created file (a new sidecar) is written
// with; an existing file keeps its own mode.
const defaultFileMode = os.FileMode(0o644)

// writeNew atomically writes data to path, preserving the file's mode when it
// already exists and defaulting a freshly created one to defaultFileMode. Unlike
// writeFile it tolerates a missing path, since a sidecar may be created for the
// first time.
func writeNew(path string, data []byte) error {
	perm := defaultFileMode
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	return xos.AtomicWrite(path, data, perm)
}

// writeFile atomically replaces path's contents with lines joined by newlines,
// preserving the file's existing mode exactly. clover never changes a file's
// permissions - managing those is the user's responsibility - so a file's mode
// is read and re-applied verbatim, and a file whose mode cannot be read is left
// untouched (an error) rather than written with an invented one.
func writeFile(path string, lines []string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	return xos.AtomicWrite(path, []byte(strings.Join(lines, "\n")), info.Mode().Perm())
}
